//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func prepareSQLiteRuntime(a *app) error {
	if override := strings.TrimSpace(os.Getenv("ARAZI_SQLITE3")); override != "" {
		a.sqliteExe = override
	} else if path, err := exec.LookPath("sqlite3"); err == nil {
		a.sqliteExe = path
	} else {
		return errors.New("test sistemi için sqlite3 komutu bulunamadı")
	}

	cmd := newHiddenCommand(a.sqliteExe, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite3 çalıştırılamadı: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	if !strings.Contains(string(output), "3.") {
		return errors.New("sqlite3 sürümü doğrulanamadı")
	}
	return nil
}

func runSQLitePlatform(a *app, databasePath, sql string, jsonMode bool) ([]byte, error) {
	args := []string{"-batch", "-bail"}
	if jsonMode {
		args = append(args, "-json")
	}
	args = append(args, databasePath)
	cmd := newHiddenCommand(a.sqliteExe, args...)
	cmd.Stdin = strings.NewReader(sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("sqlite3: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
