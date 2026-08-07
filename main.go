package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed web/*
var embeddedWeb embed.FS

const (
	appFolderName = "AraziCikisCetveli"
	dataFileName  = "arazi-cikis-verileri.db"
	legacyName    = "arazi-cikis-verileri.json"
	maxStateBytes = 16 << 20
	maxDBBytes    = 128 << 20
)

type lockInfo struct {
	URL string `json:"url"`
	PID int    `json:"pid"`
}

type dayData struct {
	Plate       string `json:"plate"`
	Place       string `json:"place"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

type monthData struct {
	Days          map[string]dayData `json:"days"`
	PreparedBy    string             `json:"preparedBy"`
	PreparedTitle string             `json:"preparedTitle"`
	ApprovedBy    string             `json:"approvedBy"`
	ApprovedTitle string             `json:"approvedTitle"`
}

type settingsData struct {
	PreparedBy    string `json:"preparedBy"`
	PreparedTitle string `json:"preparedTitle"`
	ApprovedBy    string `json:"approvedBy"`
	ApprovedTitle string `json:"approvedTitle"`
}

type appState struct {
	SelectedYear  int                  `json:"selectedYear"`
	SelectedMonth int                  `json:"selectedMonth"`
	Years         []int                `json:"years"`
	Months        map[string]monthData `json:"months"`
	Settings      settingsData         `json:"settings"`
}

type app struct {
	mu          sync.Mutex
	dataDir     string
	dataFile    string
	backupDir   string
	legacyFile  string
	previousDB  string
	importDB    string
	sqliteExe   string
	lockFile    string
	token       string
	shutdownCh  chan struct{}
	shutdownOne sync.Once
	lastSeen    atomic.Int64
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	a, err := newApp()
	if err != nil {
		showError("Uygulama başlatılamadı", err)
		return
	}

	if existingURL, ok := a.runningInstance(); ok {
		_ = openAppWindow(existingURL)
		return
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		showError("Yerel bağlantı açılamadı", err)
		return
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	appURL := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", port, url.QueryEscape(a.token))
	if err := a.createLock(appURL); err != nil {
		showError("Uygulama kilidi oluşturulamadı", err)
		return
	}
	defer os.Remove(a.lockFile)

	if err := a.ensureSQLiteRuntime(); err != nil {
		showError("SQLite veritabanı bileşeni hazırlanamadı", err)
		return
	}
	if err := a.recoverDatabase(); err != nil {
		showError("Veritabanı kurtarılamadı", err)
		return
	}
	if err := a.initDatabase(); err != nil {
		showError("SQLite veritabanı oluşturulamadı", err)
		return
	}
	if err := a.migrateLegacyJSON(); err != nil {
		showError("Eski kayıtlar SQLite'a aktarılamadı", err)
		return
	}

	_ = a.createDailyBackup()
	_ = a.pruneBackups(90)
	a.touch()

	server := &http.Server{
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("sunucu hatası: %v", err)
			a.requestShutdown()
		}
	}()

	go a.idleShutdownLoop()

	if os.Getenv("ARAZI_NO_BROWSER") == "1" {
		log.Printf("test modu: %s", appURL)
	} else if err := openAppWindow(appURL); err != nil {
		showError("Uygulama penceresi açılamadı", err)
		a.requestShutdown()
	}

	<-a.shutdownCh
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func newApp() (*app, error) {
	baseDir := strings.TrimSpace(os.Getenv("APPDATA"))

	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("kullanıcı veri klasörü bulunamadı: %w", err)
		}

		baseDir = filepath.Join(homeDir, "AppData", "Roaming")
	}

	dataDir := filepath.Join(baseDir, appFolderName)
	backupDir := filepath.Join(dataDir, "Yedekler")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, err
	}

	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}

	return &app{
		dataDir:    dataDir,
		dataFile:   filepath.Join(dataDir, dataFileName),
		backupDir:  backupDir,
		legacyFile: filepath.Join(dataDir, legacyName),
		previousDB: filepath.Join(dataDir, dataFileName+".previous"),
		importDB:   filepath.Join(dataDir, dataFileName+".import"),
		sqliteExe:  "",
		lockFile:   filepath.Join(dataDir, "app.lock"),
		token:      hex.EncodeToString(tokenBytes),
		shutdownCh: make(chan struct{}),
	}, nil
}

func (a *app) routes() http.Handler {
	webRoot, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", a.auth(a.handleHealth))
	mux.HandleFunc("/api/state", a.auth(a.handleState))
	mux.HandleFunc("/api/database", a.auth(a.handleDatabase))
	mux.HandleFunc("/api/backup", a.auth(a.handleBackup))
	mux.HandleFunc("/api/open-data-folder", a.auth(a.handleOpenDataFolder))
	mux.HandleFunc("/api/heartbeat", a.auth(a.handleHeartbeat))
	mux.HandleFunc("/api/shutdown", a.auth(a.handleShutdown))
	mux.Handle("/", http.FileServer(http.FS(webRoot)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.touch()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (a *app) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != a.token {
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Yöntem desteklenmiyor", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "database": "SQLite"})
}

func (a *app) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Yöntem desteklenmiyor", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) handleState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, err := a.readState()
		if err != nil {
			log.Printf("okuma hatası: %v", err)
			http.Error(w, "SQLite veritabanı okunamadı", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"state":        state,
			"dataPath":     a.dataFile,
			"backupPath":   a.backupDir,
			"databaseType": "SQLite",
		})

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxStateBytes+1))
		if err != nil || len(body) == 0 || len(body) > maxStateBytes {
			http.Error(w, "Geçersiz veri boyutu", http.StatusBadRequest)
			return
		}

		var state appState
		if err := json.Unmarshal(body, &state); err != nil || state.Months == nil {
			http.Error(w, "Geçersiz kayıt verisi", http.StatusBadRequest)
			return
		}
		normalizeAppState(&state)
		if err := a.writeState(state); err != nil {
			log.Printf("SQLite kayıt hatası: %v", err)
			http.Error(w, "Veriler SQLite veritabanına yazılamadı", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "path": a.dataFile, "database": "SQLite"})

	default:
		http.Error(w, "Yöntem desteklenmiyor", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleDatabase(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		defer a.mu.Unlock()
		file, err := os.Open(a.dataFile)
		if err != nil {
			http.Error(w, "Veritabanı dosyası açılamadı", http.StatusInternalServerError)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/vnd.sqlite3")
		w.Header().Set("Content-Disposition", `attachment; filename="arazi-cikis-verileri.db"`)
		_, _ = io.Copy(w, file)

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxDBBytes+1))
		if err != nil || len(body) > maxDBBytes || !isSQLiteFile(body) {
			http.Error(w, "Geçersiz SQLite yedeği", http.StatusBadRequest)
			return
		}
		if err := a.restoreDatabase(body); err != nil {
			log.Printf("yedek yükleme hatası: %v", err)
			http.Error(w, "SQLite yedeği yüklenemedi", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"restored": true})

	default:
		http.Error(w, "Yöntem desteklenmiyor", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Yöntem desteklenmiyor", http.StatusMethodNotAllowed)
		return
	}

	path, err := a.createManualBackup()
	if err != nil {
		http.Error(w, "Yedek oluşturulamadı", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": true, "path": path, "format": "SQLite"})
}

func (a *app) handleOpenDataFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Yöntem desteklenmiyor", http.StatusMethodNotAllowed)
		return
	}

	if err := openFolder(a.dataDir); err != nil {
		http.Error(w, "Klasör açılamadı", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Yöntem desteklenmiyor", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	go func() {
		time.Sleep(250 * time.Millisecond)
		a.requestShutdown()
	}()
}

func (a *app) ensureSQLiteRuntime() error {
	return prepareSQLiteRuntime(a)
}

func (a *app) initDatabase() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.initDatabaseUnlocked(a.dataFile)
}

func (a *app) initDatabaseUnlocked(path string) error {
	schema := `
PRAGMA journal_mode=DELETE;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS app_state (
  id INTEGER PRIMARY KEY CHECK(id=1),
  selected_year INTEGER NOT NULL,
  selected_month INTEGER NOT NULL CHECK(selected_month BETWEEN 0 AND 11)
);
CREATE TABLE IF NOT EXISTS years (
  year INTEGER PRIMARY KEY CHECK(year BETWEEN 2000 AND 2100)
);
CREATE TABLE IF NOT EXISTS settings (
  id INTEGER PRIMARY KEY CHECK(id=1),
  prepared_by TEXT NOT NULL DEFAULT '',
  prepared_title TEXT NOT NULL DEFAULT '',
  approved_by TEXT NOT NULL DEFAULT '',
  approved_title TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS month_signatures (
  year INTEGER NOT NULL,
  month INTEGER NOT NULL CHECK(month BETWEEN 1 AND 12),
  prepared_by TEXT NOT NULL DEFAULT '',
  prepared_title TEXT NOT NULL DEFAULT '',
  approved_by TEXT NOT NULL DEFAULT '',
  approved_title TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(year, month)
);
CREATE TABLE IF NOT EXISTS entries (
  year INTEGER NOT NULL,
  month INTEGER NOT NULL CHECK(month BETWEEN 1 AND 12),
  day INTEGER NOT NULL CHECK(day BETWEEN 1 AND 31),
  plate TEXT NOT NULL DEFAULT '',
  place TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(year, month, day)
);
CREATE INDEX IF NOT EXISTS idx_entries_period ON entries(year, month, day);
CREATE TABLE IF NOT EXISTS metadata (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR IGNORE INTO metadata(key,value) VALUES('schema_version','1');
`
	_, err := a.runSQLiteOn(path, schema, false)
	return err
}

func (a *app) readState() (*appState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.readStateUnlocked()
}

func (a *app) readStateUnlocked() (*appState, error) {
	state := &appState{Months: map[string]monthData{}}

	var appRows []struct {
		SelectedYear  int `json:"selected_year"`
		SelectedMonth int `json:"selected_month"`
	}
	if err := a.queryJSON(`SELECT selected_year, selected_month FROM app_state WHERE id=1;`, &appRows); err != nil {
		return nil, err
	}
	if len(appRows) == 0 {
		return nil, nil
	}
	state.SelectedYear = appRows[0].SelectedYear
	state.SelectedMonth = appRows[0].SelectedMonth

	var yearRows []struct {
		Year int `json:"year"`
	}
	if err := a.queryJSON(`SELECT year FROM years ORDER BY year;`, &yearRows); err != nil {
		return nil, err
	}
	for _, row := range yearRows {
		state.Years = append(state.Years, row.Year)
	}

	var settingRows []struct {
		PreparedBy    string `json:"prepared_by"`
		PreparedTitle string `json:"prepared_title"`
		ApprovedBy    string `json:"approved_by"`
		ApprovedTitle string `json:"approved_title"`
	}
	if err := a.queryJSON(`SELECT prepared_by, prepared_title, approved_by, approved_title FROM settings WHERE id=1;`, &settingRows); err != nil {
		return nil, err
	}
	if len(settingRows) > 0 {
		state.Settings = settingsData{
			PreparedBy: settingRows[0].PreparedBy, PreparedTitle: settingRows[0].PreparedTitle,
			ApprovedBy: settingRows[0].ApprovedBy, ApprovedTitle: settingRows[0].ApprovedTitle,
		}
	}

	var signatureRows []struct {
		Year          int    `json:"year"`
		Month         int    `json:"month"`
		PreparedBy    string `json:"prepared_by"`
		PreparedTitle string `json:"prepared_title"`
		ApprovedBy    string `json:"approved_by"`
		ApprovedTitle string `json:"approved_title"`
	}
	if err := a.queryJSON(`SELECT year, month, prepared_by, prepared_title, approved_by, approved_title FROM month_signatures ORDER BY year, month;`, &signatureRows); err != nil {
		return nil, err
	}
	for _, row := range signatureRows {
		key := fmt.Sprintf("%d-%d", row.Year, row.Month)
		state.Months[key] = monthData{
			Days: map[string]dayData{}, PreparedBy: row.PreparedBy, PreparedTitle: row.PreparedTitle,
			ApprovedBy: row.ApprovedBy, ApprovedTitle: row.ApprovedTitle,
		}
	}

	var entryRows []struct {
		Year        int    `json:"year"`
		Month       int    `json:"month"`
		Day         int    `json:"day"`
		Plate       string `json:"plate"`
		Place       string `json:"place"`
		Subject     string `json:"subject"`
		Description string `json:"description"`
	}
	if err := a.queryJSON(`SELECT year, month, day, plate, place, subject, description FROM entries ORDER BY year, month, day;`, &entryRows); err != nil {
		return nil, err
	}
	for _, row := range entryRows {
		key := fmt.Sprintf("%d-%d", row.Year, row.Month)
		month := state.Months[key]
		if month.Days == nil {
			month.Days = map[string]dayData{}
		}
		month.Days[strconv.Itoa(row.Day)] = dayData{
			Plate: row.Plate, Place: row.Place, Subject: row.Subject, Description: row.Description,
		}
		state.Months[key] = month
	}

	normalizeAppState(state)
	return state, nil
}

func (a *app) queryJSON(sql string, target any) error {
	output, err := a.runSQLiteOn(a.dataFile, sql, true)
	if err != nil {
		return err
	}
	trimmed := bytesTrimSpace(output)
	if len(trimmed) == 0 {
		trimmed = []byte("[]")
	}
	return json.Unmarshal(trimmed, target)
}

func (a *app) writeState(state appState) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.writeStateUnlocked(state)
}

func (a *app) writeStateUnlocked(state appState) error {
	normalizeAppState(&state)
	var sql strings.Builder
	sql.WriteString("PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; BEGIN IMMEDIATE;\n")
	sql.WriteString("DELETE FROM entries; DELETE FROM month_signatures; DELETE FROM years; DELETE FROM app_state; DELETE FROM settings;\n")
	fmt.Fprintf(&sql, "INSERT INTO app_state(id,selected_year,selected_month) VALUES(1,%d,%d);\n", state.SelectedYear, state.SelectedMonth)
	fmt.Fprintf(&sql, "INSERT INTO settings(id,prepared_by,prepared_title,approved_by,approved_title) VALUES(1,%s,%s,%s,%s);\n",
		sqlText(state.Settings.PreparedBy), sqlText(state.Settings.PreparedTitle), sqlText(state.Settings.ApprovedBy), sqlText(state.Settings.ApprovedTitle))

	for _, year := range state.Years {
		if year >= 2000 && year <= 2100 {
			fmt.Fprintf(&sql, "INSERT OR IGNORE INTO years(year) VALUES(%d);\n", year)
		}
	}
	for key, month := range state.Months {
		year, monthNumber, ok := parseMonthKey(key)
		if !ok {
			continue
		}
		fmt.Fprintf(&sql, "INSERT INTO month_signatures(year,month,prepared_by,prepared_title,approved_by,approved_title) VALUES(%d,%d,%s,%s,%s,%s);\n",
			year, monthNumber, sqlText(month.PreparedBy), sqlText(month.PreparedTitle), sqlText(month.ApprovedBy), sqlText(month.ApprovedTitle))
		for dayKey, day := range month.Days {
			dayNumber, err := strconv.Atoi(dayKey)
			if err != nil || dayNumber < 1 || dayNumber > 31 || !hasDayValues(day) {
				continue
			}
			fmt.Fprintf(&sql, "INSERT INTO entries(year,month,day,plate,place,subject,description) VALUES(%d,%d,%d,%s,%s,%s,%s);\n",
				year, monthNumber, dayNumber, sqlText(day.Plate), sqlText(day.Place), sqlText(day.Subject), sqlText(day.Description))
		}
	}
	sql.WriteString("INSERT OR REPLACE INTO metadata(key,value) VALUES('last_saved_at',datetime('now')); COMMIT;\n")
	_, err := a.runSQLiteOn(a.dataFile, sql.String(), false)
	if err == nil {
		_ = a.createDailyBackupUnlocked()
	}
	return err
}

func (a *app) runSQLiteOn(databasePath, sql string, jsonMode bool) ([]byte, error) {
	return runSQLitePlatform(a, databasePath, sql, jsonMode)
}

func (a *app) migrateLegacyJSON() error {
	if _, err := os.Stat(a.legacyFile); err != nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	current, err := a.readStateUnlocked()
	if err != nil {
		return err
	}
	if current != nil {
		return nil
	}
	body, err := os.ReadFile(a.legacyFile)
	if err != nil {
		return err
	}
	var legacy appState
	if err := json.Unmarshal(body, &legacy); err != nil {
		return fmt.Errorf("eski JSON verisi geçersiz: %w", err)
	}
	normalizeAppState(&legacy)
	if err := a.writeStateUnlocked(legacy); err != nil {
		return err
	}
	migratedName := a.legacyFile + ".migrated-" + time.Now().Format("20060102-150405")
	return os.Rename(a.legacyFile, migratedName)
}

func (a *app) restoreDatabase(body []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := os.WriteFile(a.importDB, body, 0o600); err != nil {
		return err
	}
	defer os.Remove(a.importDB)

	output, err := a.runSQLiteOn(a.importDB, "PRAGMA quick_check;", false)
	if err != nil || strings.TrimSpace(string(output)) != "ok" {
		return errors.New("SQLite bütünlük kontrolü başarısız")
	}
	var rows []struct {
		Count int `json:"count"`
	}
	query := `SELECT count(*) AS count FROM sqlite_master WHERE type='table' AND name IN ('app_state','years','settings','month_signatures','entries');`
	jsonOut, err := a.runSQLiteOn(a.importDB, query, true)
	if err != nil || json.Unmarshal(bytesTrimSpace(jsonOut), &rows) != nil || len(rows) != 1 || rows[0].Count != 5 {
		return errors.New("bu dosya Arazi Çıkış Cetveli SQLite yedeği değil")
	}

	if _, err := os.Stat(a.dataFile); err == nil {
		_ = copyFile(a.dataFile, a.previousDB)
	}
	if err := copyFile(a.importDB, a.dataFile); err != nil {
		return err
	}
	return a.initDatabaseUnlocked(a.dataFile)
}

func (a *app) recoverDatabase() error {
	if _, err := os.Stat(a.dataFile); err == nil {
		_ = os.Remove(a.importDB)
		return nil
	}
	if _, err := os.Stat(a.previousDB); err == nil {
		return copyFile(a.previousDB, a.dataFile)
	}
	return nil
}

func (a *app) createDailyBackup() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.createDailyBackupUnlocked()
}

func (a *app) createDailyBackupUnlocked() error {
	if _, err := os.Stat(a.dataFile); err != nil {
		return nil
	}
	path := filepath.Join(a.backupDir, "otomatik-"+time.Now().Format("2006-01-02")+".db")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return copyFile(a.dataFile, path)
}

func (a *app) createManualBackup() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, err := os.Stat(a.dataFile); err != nil {
		return "", err
	}
	path := filepath.Join(a.backupDir, "manuel-"+time.Now().Format("2006-01-02_15-04-05")+".db")
	if err := copyFile(a.dataFile, path); err != nil {
		return "", err
	}
	return path, nil
}

func (a *app) pruneBackups(keep int) error {
	entries, err := os.ReadDir(a.backupDir)
	if err != nil {
		return err
	}
	type item struct {
		path string
		mod  time.Time
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, item{path: filepath.Join(a.backupDir, entry.Name()), mod: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	if len(items) <= keep {
		return nil
	}
	for _, old := range items[keep:] {
		_ = os.Remove(old.path)
	}
	return nil
}

func normalizeAppState(state *appState) {
	now := time.Now()
	if state.SelectedYear < 2000 || state.SelectedYear > 2100 {
		state.SelectedYear = now.Year()
	}
	if state.SelectedMonth < 0 || state.SelectedMonth > 11 {
		state.SelectedMonth = int(now.Month()) - 1
	}
	if state.Months == nil {
		state.Months = map[string]monthData{}
	}
	yearSet := map[int]bool{state.SelectedYear: true}
	for _, year := range state.Years {
		if year >= 2000 && year <= 2100 {
			yearSet[year] = true
		}
	}
	for key, month := range state.Months {
		year, _, ok := parseMonthKey(key)
		if !ok {
			delete(state.Months, key)
			continue
		}
		yearSet[year] = true
		if month.Days == nil {
			month.Days = map[string]dayData{}
		}
		state.Months[key] = month
	}
	state.Years = state.Years[:0]
	for year := range yearSet {
		state.Years = append(state.Years, year)
	}
	sort.Ints(state.Years)
}

func parseMonthKey(key string) (int, int, bool) {
	parts := strings.Split(key, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	year, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	return year, month, err1 == nil && err2 == nil && year >= 2000 && year <= 2100 && month >= 1 && month <= 12
}

func hasDayValues(day dayData) bool {
	return strings.TrimSpace(day.Plate) != "" || strings.TrimSpace(day.Place) != "" || strings.TrimSpace(day.Subject) != "" || strings.TrimSpace(day.Description) != ""
}

func sqlText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func isSQLiteFile(body []byte) bool {
	return len(body) >= 16 && string(body[:16]) == "SQLite format 3\x00"
}

func bytesTrimSpace(data []byte) []byte {
	return []byte(strings.TrimSpace(string(data)))
}

func copyFile(src, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()

	temp := dst + ".tmp"
	output, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(temp)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(temp)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(temp)
		return closeErr
	}
	_ = os.Remove(dst)
	return os.Rename(temp, dst)
}

func (a *app) runningInstance() (string, bool) {
	body, err := os.ReadFile(a.lockFile)
	if err != nil {
		return "", false
	}

	var info lockInfo
	if json.Unmarshal(body, &info) != nil || info.URL == "" {
		_ = os.Remove(a.lockFile)
		return "", false
	}

	u, err := url.Parse(info.URL)
	if err != nil {
		_ = os.Remove(a.lockFile)
		return "", false
	}
	token := u.Query().Get("token")
	healthURL := fmt.Sprintf("%s://%s/api/health?token=%s", u.Scheme, u.Host, url.QueryEscape(token))
	client := &http.Client{Timeout: 800 * time.Millisecond}
	response, err := client.Get(healthURL)
	if err == nil {
		_ = response.Body.Close()
		if response.StatusCode == http.StatusOK {
			return info.URL, true
		}
	}

	_ = os.Remove(a.lockFile)
	return "", false
}

func (a *app) createLock(appURL string) error {
	payload, _ := json.Marshal(lockInfo{URL: appURL, PID: os.Getpid()})
	file, err := os.OpenFile(a.lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(payload)
	return err
}

func (a *app) touch() {
	a.lastSeen.Store(time.Now().Unix())
}

func (a *app) idleShutdownLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			last := time.Unix(a.lastSeen.Load(), 0)
			if time.Since(last) > 10*time.Minute {
				a.requestShutdown()
				return
			}
		case <-a.shutdownCh:
			return
		}
	}
}

func (a *app) requestShutdown() {
	a.shutdownOne.Do(func() { close(a.shutdownCh) })
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func openAppWindow(appURL string) error {
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Edge", "Application", "msedge.exe"),
		}
		for _, edge := range candidates {
			if edge == "" {
				continue
			}
			if _, err := os.Stat(edge); err == nil {
				return exec.Command(edge, "--app="+appURL, "--start-maximized").Start()
			}
		}
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", appURL).Start()
	}

	if runtime.GOOS == "darwin" {
		return exec.Command("open", appURL).Start()
	}
	return exec.Command("xdg-open", appURL).Start()
}

func openFolder(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func showError(title string, err error) {
	message := title + ":\n" + err.Error()
	if runtime.GOOS == "windows" {
		_ = exec.Command("powershell", "-NoProfile", "-Command",
			"Add-Type -AssemblyName PresentationFramework; [System.Windows.MessageBox]::Show($args[0], $args[1])",
			message, "Arazi Çıkış Cetveli").Run()
		return
	}
	fmt.Fprintln(os.Stderr, message)
}
