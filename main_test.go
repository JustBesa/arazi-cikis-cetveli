package main

import (
	"reflect"
	"testing"
)

func TestParseMonthKey(t *testing.T) {
	year, month, ok := parseMonthKey("2026-8")
	if !ok || year != 2026 || month != 8 {
		t.Fatalf("beklenmeyen sonuç: %d %d %v", year, month, ok)
	}

	for _, invalid := range []string{"2026", "1999-1", "2026-0", "2026-13", "x-1"} {
		if _, _, ok := parseMonthKey(invalid); ok {
			t.Fatalf("geçersiz anahtar kabul edildi: %q", invalid)
		}
	}
}

func TestSQLTextEscapesApostropheAndNull(t *testing.T) {
	got := sqlText("O'Brien\x00\nYolboyu")
	want := "'O''Brien\nYolboyu'"
	if got != want {
		t.Fatalf("sqlText=%q, beklenen=%q", got, want)
	}
}

func TestNormalizeAppState(t *testing.T) {
	state := appState{
		SelectedYear:  2026,
		SelectedMonth: 7,
		Years:         []int{2027, 2026, 2027, 1900},
		Months: map[string]monthData{
			"2025-12": {},
			"hatalı":  {},
		},
	}
	normalizeAppState(&state)
	if !reflect.DeepEqual(state.Years, []int{2025, 2026, 2027}) {
		t.Fatalf("yıllar=%v", state.Years)
	}
	if _, exists := state.Months["hatalı"]; exists {
		t.Fatal("geçersiz ay anahtarı silinmedi")
	}
	if state.Months["2025-12"].Days == nil {
		t.Fatal("ay gün haritası hazırlanmadı")
	}
}
