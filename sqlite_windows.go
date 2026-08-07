//go:build windows

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	sqliteOK      = 0
	sqliteInteger = 1
	sqliteFloat   = 2
	sqliteText    = 3
	sqliteBlob    = 4
	sqliteNull    = 5
	sqliteRow     = 100
	sqliteDone    = 101

	sqliteOpenReadWrite = 0x00000002
	sqliteOpenCreate    = 0x00000004
	sqliteOpenFullMutex = 0x00010000
)

var nativeSQLite = struct {
	dll *syscall.LazyDLL

	libVersion  *syscall.LazyProc
	openV2      *syscall.LazyProc
	closeV2     *syscall.LazyProc
	errMsg      *syscall.LazyProc
	busyTimeout *syscall.LazyProc
	prepareV2   *syscall.LazyProc
	step        *syscall.LazyProc
	finalize    *syscall.LazyProc
	columnCount *syscall.LazyProc
	columnName  *syscall.LazyProc
	columnType  *syscall.LazyProc
	columnInt64 *syscall.LazyProc
	columnText  *syscall.LazyProc
	columnBlob  *syscall.LazyProc
	columnBytes *syscall.LazyProc
}{
	dll: syscall.NewLazyDLL("winsqlite3.dll"),
}

func init() {
	nativeSQLite.libVersion = nativeSQLite.dll.NewProc("sqlite3_libversion")
	nativeSQLite.openV2 = nativeSQLite.dll.NewProc("sqlite3_open_v2")
	nativeSQLite.closeV2 = nativeSQLite.dll.NewProc("sqlite3_close_v2")
	nativeSQLite.errMsg = nativeSQLite.dll.NewProc("sqlite3_errmsg")
	nativeSQLite.busyTimeout = nativeSQLite.dll.NewProc("sqlite3_busy_timeout")
	nativeSQLite.prepareV2 = nativeSQLite.dll.NewProc("sqlite3_prepare_v2")
	nativeSQLite.step = nativeSQLite.dll.NewProc("sqlite3_step")
	nativeSQLite.finalize = nativeSQLite.dll.NewProc("sqlite3_finalize")
	nativeSQLite.columnCount = nativeSQLite.dll.NewProc("sqlite3_column_count")
	nativeSQLite.columnName = nativeSQLite.dll.NewProc("sqlite3_column_name")
	nativeSQLite.columnType = nativeSQLite.dll.NewProc("sqlite3_column_type")
	nativeSQLite.columnInt64 = nativeSQLite.dll.NewProc("sqlite3_column_int64")
	nativeSQLite.columnText = nativeSQLite.dll.NewProc("sqlite3_column_text")
	nativeSQLite.columnBlob = nativeSQLite.dll.NewProc("sqlite3_column_blob")
	nativeSQLite.columnBytes = nativeSQLite.dll.NewProc("sqlite3_column_bytes")
}

func prepareSQLiteRuntime(a *app) error {
	_ = a
	if err := nativeSQLite.dll.Load(); err != nil {
		return fmt.Errorf("Windows SQLite bileşeni yüklenemedi: %w", err)
	}
	required := []*syscall.LazyProc{
		nativeSQLite.libVersion, nativeSQLite.openV2, nativeSQLite.closeV2,
		nativeSQLite.errMsg, nativeSQLite.busyTimeout, nativeSQLite.prepareV2,
		nativeSQLite.step, nativeSQLite.finalize, nativeSQLite.columnCount,
		nativeSQLite.columnName, nativeSQLite.columnType, nativeSQLite.columnInt64,
		nativeSQLite.columnText, nativeSQLite.columnBlob,
		nativeSQLite.columnBytes,
	}
	for _, proc := range required {
		if err := proc.Find(); err != nil {
			return fmt.Errorf("Windows SQLite API'si bulunamadı: %w", err)
		}
	}
	ptr, _, _ := nativeSQLite.libVersion.Call()
	version := cString(ptr)
	if ptr == 0 || !strings.HasPrefix(version, "3.") {
		return errors.New("Windows SQLite sürümü doğrulanamadı")
	}
	return nil
}

func runSQLitePlatform(a *app, databasePath, sql string, jsonMode bool) ([]byte, error) {
	_ = a
	pathPtr, err := syscall.BytePtrFromString(databasePath)
	if err != nil {
		return nil, fmt.Errorf("veritabanı yolu geçersiz: %w", err)
	}

	var db uintptr
	rc, _, _ := nativeSQLite.openV2.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&db)),
		sqliteOpenReadWrite|sqliteOpenCreate|sqliteOpenFullMutex,
		0,
	)
	runtime.KeepAlive(pathPtr)
	if int32(rc) != sqliteOK {
		message := sqliteError(db)
		if db != 0 {
			_, _, _ = nativeSQLite.closeV2.Call(db)
		}
		return nil, fmt.Errorf("SQLite veritabanı açılamadı (%d): %s", int32(rc), message)
	}
	defer nativeSQLite.closeV2.Call(db)
	_, _, _ = nativeSQLite.busyTimeout.Call(db, 5000)

	sqlBytes := append([]byte(sql), 0)
	if len(sqlBytes) == 1 {
		if jsonMode {
			return []byte("[]"), nil
		}
		return nil, nil
	}

	base := uintptr(unsafe.Pointer(&sqlBytes[0]))
	end := base + uintptr(len(sqlBytes)-1)
	current := base
	jsonRows := make([]map[string]any, 0)
	var textOutput strings.Builder

	for current < end {
		var statement uintptr
		var tail uintptr
		remaining := end - current
		rc, _, _ = nativeSQLite.prepareV2.Call(
			db,
			current,
			remaining,
			uintptr(unsafe.Pointer(&statement)),
			uintptr(unsafe.Pointer(&tail)),
		)
		if int32(rc) != sqliteOK {
			runtime.KeepAlive(sqlBytes)
			return nil, fmt.Errorf("SQLite sorgusu hazırlanamadı (%d): %s", int32(rc), sqliteError(db))
		}

		if statement == 0 {
			if tail <= current {
				break
			}
			current = tail
			continue
		}

		columnCount, _, _ := nativeSQLite.columnCount.Call(statement)
		stepErr := executeNativeStatement(db, statement, int(columnCount), jsonMode, &jsonRows, &textOutput)
		finalRC, _, _ := nativeSQLite.finalize.Call(statement)
		if stepErr != nil {
			runtime.KeepAlive(sqlBytes)
			return nil, stepErr
		}
		if int32(finalRC) != sqliteOK {
			runtime.KeepAlive(sqlBytes)
			return nil, fmt.Errorf("SQLite sorgusu tamamlanamadı (%d): %s", int32(finalRC), sqliteError(db))
		}

		if tail <= current {
			break
		}
		current = tail
	}
	runtime.KeepAlive(sqlBytes)

	if jsonMode {
		output, err := json.Marshal(jsonRows)
		if err != nil {
			return nil, err
		}
		return output, nil
	}
	return []byte(textOutput.String()), nil
}

func executeNativeStatement(db, statement uintptr, columnCount int, jsonMode bool, jsonRows *[]map[string]any, textOutput *strings.Builder) error {
	for {
		rc, _, _ := nativeSQLite.step.Call(statement)
		switch int32(rc) {
		case sqliteRow:
			if jsonMode {
				row := make(map[string]any, columnCount)
				for column := 0; column < columnCount; column++ {
					namePtr, _, _ := nativeSQLite.columnName.Call(statement, uintptr(column))
					name := cString(namePtr)
					row[name] = nativeColumnValue(statement, column)
				}
				*jsonRows = append(*jsonRows, row)
			} else if columnCount > 0 {
				for column := 0; column < columnCount; column++ {
					if column > 0 {
						textOutput.WriteByte('|')
					}
					textOutput.WriteString(nativeColumnString(statement, column))
				}
				textOutput.WriteByte('\n')
			}
		case sqliteDone:
			return nil
		default:
			return fmt.Errorf("SQLite sorgusu çalıştırılamadı (%d): %s", int32(rc), sqliteError(db))
		}
	}
}

func nativeColumnValue(statement uintptr, column int) any {
	kind, _, _ := nativeSQLite.columnType.Call(statement, uintptr(column))
	switch int32(kind) {
	case sqliteInteger:
		value, _, _ := nativeSQLite.columnInt64.Call(statement, uintptr(column))
		return int64(value)
	case sqliteFloat:
		ptr, _, _ := nativeSQLite.columnText.Call(statement, uintptr(column))
		length, _, _ := nativeSQLite.columnBytes.Call(statement, uintptr(column))
		text := pointerString(ptr, int(length))
		if value, err := strconv.ParseFloat(text, 64); err == nil {
			return value
		}
		return text
	case sqliteText:
		ptr, _, _ := nativeSQLite.columnText.Call(statement, uintptr(column))
		length, _, _ := nativeSQLite.columnBytes.Call(statement, uintptr(column))
		return pointerString(ptr, int(length))
	case sqliteBlob:
		ptr, _, _ := nativeSQLite.columnBlob.Call(statement, uintptr(column))
		length, _, _ := nativeSQLite.columnBytes.Call(statement, uintptr(column))
		if ptr == 0 || length == 0 {
			return ""
		}
		data := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(length))...)
		return hex.EncodeToString(data)
	case sqliteNull:
		return nil
	default:
		return nil
	}
}

func nativeColumnString(statement uintptr, column int) string {
	value := nativeColumnValue(statement, column)
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func sqliteError(db uintptr) string {
	if db == 0 {
		return "bilinmeyen SQLite hatası"
	}
	ptr, _, _ := nativeSQLite.errMsg.Call(db)
	if message := cString(ptr); message != "" {
		return message
	}
	return "bilinmeyen SQLite hatası"
}

func cString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	const maxCString = 1 << 20
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), maxCString)
	for i, value := range bytes {
		if value == 0 {
			return string(bytes[:i])
		}
	}
	return string(bytes)
}

func pointerString(ptr uintptr, length int) string {
	if ptr == 0 || length <= 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), length))
}
