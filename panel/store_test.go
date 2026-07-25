package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := s.Get().DeviceKey
	if key == "" {
		t.Fatal("no device key issued on first start")
	}
	if err := s.Save("Kitchen Panel", []Row{
		{"Bin Night", "Tuesday"},
		{"  ", "  "}, // dropped
		{"Battery", "3.94 V"},
	}, "study panel"); err != nil {
		t.Fatal(err)
	}

	// Reopen from disk: a restart must preserve everything, key included.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Get()
	if got.Title != "Kitchen Panel" || got.Footer != "study panel" {
		t.Fatalf("title/footer lost: %+v", got)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("want 2 rows after dropping the blank, got %d: %+v", len(got.Rows), got.Rows)
	}
	if got.Rows[1] != (Row{"Battery", "3.94 V"}) {
		t.Fatalf("row 1 = %+v", got.Rows[1])
	}
	if got.DeviceKey != key {
		t.Fatal("device key changed across restart")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not set")
	}
}

// The atomic write must never leave a partial or temporary file behind.
func TestPersistIsAtomic(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := s.Save("t", []Row{{"a", "b"}}, "f"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "panel.json" {
		t.Fatalf("temp files left behind: %v", names)
	}
	info, err := os.Stat(filepath.Join(dir, "panel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("panel.json mode = %v, want 0600 (it holds the device key)", perm)
	}
}

func TestStoreEnforcesCaps(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]Row, maxRows+10)
	for i := range rows {
		rows[i] = Row{Label: strings.Repeat("L", maxLabelLen+40), Value: strings.Repeat("V", maxValueLen+40)}
	}
	if err := s.Save(strings.Repeat("T", maxTitleLen+40), rows, strings.Repeat("F", maxFooterLen+40)); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	if len(got.Rows) != maxRows {
		t.Errorf("rows = %d, want cap %d", len(got.Rows), maxRows)
	}
	if len([]rune(got.Title)) != maxTitleLen {
		t.Errorf("title len = %d, want %d", len([]rune(got.Title)), maxTitleLen)
	}
	if len([]rune(got.Footer)) != maxFooterLen {
		t.Errorf("footer len = %d, want %d", len([]rune(got.Footer)), maxFooterLen)
	}
	for _, r := range got.Rows {
		if len([]rune(r.Label)) != maxLabelLen || len([]rune(r.Value)) != maxValueLen {
			t.Fatalf("row not clipped: %d/%d", len([]rune(r.Label)), len([]rune(r.Value)))
		}
	}
}

func TestGetReturnsACopy(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("t", []Row{{"a", "b"}}, ""); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	got.Rows[0].Value = "mutated"
	if s.Get().Rows[0].Value != "b" {
		t.Fatal("Get() handed out the live slice")
	}
}

func TestRotateKeyChangesIt(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Get().DeviceKey
	next, err := s.RotateKey()
	if err != nil {
		t.Fatal(err)
	}
	if next == old {
		t.Fatal("rotate returned the same key")
	}
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Get().DeviceKey != next {
		t.Fatal("rotated key was not persisted")
	}
}

func TestDeviceKeyShape(t *testing.T) {
	const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		k, err := newDeviceKey()
		if err != nil {
			t.Fatal(err)
		}
		// 128 bits of entropy in base32 is 26 characters.
		if len(k) != 26 {
			t.Fatalf("key %q has length %d, want 26 (128 bits)", k, len(k))
		}
		for _, r := range k {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("key %q contains an ambiguous character %q", k, r)
			}
		}
		if seen[k] {
			t.Fatalf("duplicate key %q after %d draws", k, i)
		}
		seen[k] = true
	}
}

func TestClean(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"  hello   world  ", 40, "hello world"},
		{"line\nbreak", 40, "line break"},
		{"nul\x00byte", 40, "nulbyte"},
		{"escape\x1b[31m", 40, "escape[31m"},
		{"truncate me please", 8, "truncate"},
		{"", 10, ""},
		{"café — ok", 40, "café — ok"},
	}
	for _, tc := range tests {
		if got := clean(tc.in, tc.max); got != tc.want {
			t.Errorf("clean(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestNewStoreRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "panel.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(dir); err == nil {
		t.Fatal("expected an error on a corrupt state file, got none")
	}
}
