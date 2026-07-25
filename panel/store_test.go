package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstStartMakesAUsableScreen(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sc := s.DefaultScreen()
	if sc.Name != "default" || sc.W != 800 || sc.H != 480 {
		t.Fatalf("unexpected first screen: %+v", sc)
	}
	if sc.DeviceKey == "" {
		t.Fatal("no device key issued on first start")
	}
	if len(sc.Blocks) == 0 {
		t.Fatal("a brand new panel is a blank rectangle")
	}
	if err := validateScreen(sc, nil); err != nil {
		t.Fatalf("the screen shipped on first start is invalid: %v", err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := s.DefaultScreen().DeviceKey

	sc := s.DefaultScreen()
	sc.Blocks = []Block{
		{Type: BlockText, X: 10, Y: 10, W: 200, H: 40, Text: "Hallway", Font: "go-bold", Size: 24, Anchor: "nw", Align: "left"},
		{Type: BlockRows, X: 10, Y: 60, W: 400, H: 200, Font: "pixel8x16", Size: 16, Anchor: "nw", Align: "left",
			Rows: []Row{{"Bin Night", "Tuesday"}, {"Battery", "3.94 V"}}},
	}
	sc.Rotate = 90
	if err := s.SaveScreen("default", sc); err != nil {
		t.Fatal(err)
	}

	// Reopen from disk: a restart must preserve everything, key included.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.DefaultScreen()
	if len(got.Blocks) != 2 {
		t.Fatalf("blocks lost: %+v", got.Blocks)
	}
	if got.Blocks[0].Text != "Hallway" || got.Blocks[1].Rows[1].Value != "3.94 V" {
		t.Fatalf("block content lost: %+v", got.Blocks)
	}
	if got.Rotate != 90 {
		t.Errorf("rotation lost: %d", got.Rotate)
	}
	if got.DeviceKey != key {
		t.Fatal("device key changed across restart")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not set")
	}
}

// The old single-layout document must come back as a screen that looks the
// same, keeping the device key so a flashed panel does not go dark.
func TestLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
	  "title": "Kitchen Panel",
	  "rows": [{"label":"Bin Night","value":"Tuesday"},{"label":"Battery","value":"3.94 V"}],
	  "footer": "study panel",
	  "device_key": "abcdefghijklmnopqrstuvwxyz",
	  "updated_at": "2026-07-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(dir, "panel.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	sc := s.DefaultScreen()
	if sc.DeviceKey != "abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("device key not carried across: %q", sc.DeviceKey)
	}
	if sc.W != 800 || sc.H != 480 {
		t.Errorf("migrated to %dx%d", sc.W, sc.H)
	}
	kinds := map[string]int{}
	for _, b := range sc.Blocks {
		kinds[b.Type]++
	}
	for _, want := range []string{BlockText, BlockClock, BlockDate, BlockDivider, BlockRows} {
		if kinds[want] == 0 {
			t.Errorf("migration produced no %s block: %+v", want, kinds)
		}
	}
	var rowsBlock Block
	for _, b := range sc.Blocks {
		if b.Type == BlockRows {
			rowsBlock = b
		}
	}
	if len(rowsBlock.Rows) != 2 || rowsBlock.Rows[0].Label != "Bin Night" {
		t.Errorf("rows lost in migration: %+v", rowsBlock.Rows)
	}
	if err := validateScreen(sc, nil); err != nil {
		t.Errorf("migrated screen is invalid: %v", err)
	}
	// And it is written back in the new shape.
	raw, err := os.ReadFile(filepath.Join(dir, "panel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"screens"`) {
		t.Error("the migrated document was not persisted")
	}
}

func TestPersistIsAtomic(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sc := s.DefaultScreen()
	for i := 0; i < 20; i++ {
		sc.Blocks = []Block{{Type: BlockText, X: i, Y: 0, W: 100, H: 20,
			Text: "x", Font: "pixel8x16", Size: 16, Anchor: "nw", Align: "left"}}
		if err := s.SaveScreen(sc.Name, sc); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// panel.json, and nothing else: no temp files, no half-written state.
	if len(names) != 1 || names[0] != "panel.json" {
		t.Fatalf("temp files left behind: %v", names)
	}
	info, err := os.Stat(filepath.Join(dir, "panel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("panel.json mode = %v, want 0600 (it holds the device keys)", perm)
	}
}

func TestScreenLifecycle(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hall, err := s.AddScreen("Hallway Panel", 400, 300)
	if err != nil {
		t.Fatal(err)
	}
	if hall.Name != "hallway-panel" {
		t.Errorf("name not slugged: %q", hall.Name)
	}
	if hall.DeviceKey == s.DefaultScreen().DeviceKey {
		t.Error("the new screen shares a device key with the first one")
	}
	if _, err := s.AddScreen("hallway-panel", 400, 300); err == nil {
		t.Error("a duplicate name was accepted")
	}
	if _, err := s.AddScreen("!!!", 400, 300); err == nil {
		t.Error("an unusable name was accepted")
	}

	dup, err := s.DuplicateScreen("hallway-panel")
	if err != nil {
		t.Fatal(err)
	}
	if dup.Name != "hallway-panel-copy" {
		t.Errorf("duplicate name = %q", dup.Name)
	}
	if dup.DeviceKey == hall.DeviceKey {
		t.Error("a duplicated screen kept the original's device key")
	}

	if err := s.DeleteScreen("hallway-panel-copy"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Screen("hallway-panel-copy"); ok {
		t.Error("delete did not take")
	}
	if err := s.DeleteScreen("hallway-panel"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteScreen("default"); err == nil {
		t.Error("the last screen was deleted")
	}

	// The cap.
	for i := 0; i < maxScreens+2; i++ {
		_, err = s.AddScreen("s"+itoa(i), 400, 300)
	}
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Errorf("screen cap not enforced: %v", err)
	}
}

func TestSaveScreenRejectsBadBlocks(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sc := s.DefaultScreen()
	before := len(sc.Blocks)
	sc.Blocks = []Block{{Type: "wormhole", X: 0, Y: 0, W: 10, H: 10}}
	err = s.SaveScreen(sc.Name, sc)
	if err == nil {
		t.Fatal("a bad block was saved")
	}
	if !strings.Contains(err.Error(), "unknown block type") {
		t.Errorf("unhelpful error: %v", err)
	}
	if len(s.DefaultScreen().Blocks) != before {
		t.Error("a rejected save still modified the stored screen")
	}
}

func TestReplaceStateKeepsDeviceKeysByName(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := s.DefaultScreen().DeviceKey

	next := State{Screens: []Screen{
		{Name: "default", W: 400, H: 300, Blocks: []Block{
			{Type: BlockText, X: 0, Y: 0, W: 200, H: 40, Text: "pasted", Font: "go", Size: 20, Anchor: "nw", Align: "left"},
		}},
		{Name: "second", W: 800, H: 480, Blocks: []Block{}},
	}}
	if err := s.ReplaceState(next); err != nil {
		t.Fatal(err)
	}
	if got := s.DefaultScreen().DeviceKey; got != key {
		t.Errorf("device key changed on a paste: %q != %q", got, key)
	}
	if s.DefaultScreen().W != 400 {
		t.Error("the pasted dimensions did not land")
	}
	second, ok := s.Screen("second")
	if !ok {
		t.Fatal("the new screen is missing")
	}
	if second.DeviceKey == "" || second.DeviceKey == key {
		t.Errorf("the new screen has a bad device key: %q", second.DeviceKey)
	}

	// A bad paste changes nothing.
	bad := State{Screens: []Screen{{Name: "default", W: 400, H: 300, Blocks: []Block{
		{Type: BlockBox, X: 390, Y: 0, W: 200, H: 40},
	}}}}
	err = s.ReplaceState(bad)
	if err == nil {
		t.Fatal("an off-canvas block was accepted")
	}
	if !strings.Contains(err.Error(), "past the right edge") {
		t.Errorf("unhelpful error: %v", err)
	}
	if _, ok := s.Screen("second"); !ok {
		t.Error("a rejected paste still modified the state")
	}

	// Two screens of the same name.
	err = s.ReplaceState(State{Screens: []Screen{
		{Name: "a", W: 400, H: 300}, {Name: "a", W: 400, H: 300},
	}})
	if err == nil || !strings.Contains(err.Error(), "both named") {
		t.Errorf("duplicate names accepted: %v", err)
	}
}

func TestGetReturnsACopy(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got := s.DefaultScreen()
	if len(got.Blocks) == 0 {
		t.Skip("nothing to mutate")
	}
	got.Blocks[0].Text = "mutated"
	got.Blocks[0].X = 9999
	if s.DefaultScreen().Blocks[0].X == 9999 {
		t.Fatal("DefaultScreen handed out the live slice")
	}
}

func TestRotateKeyChangesOnlyThatScreen(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddScreen("other", 400, 300)
	if err != nil {
		t.Fatal(err)
	}
	old := s.DefaultScreen().DeviceKey
	next, err := s.RotateKey("default")
	if err != nil {
		t.Fatal(err)
	}
	if next == old {
		t.Fatal("rotate returned the same key")
	}
	if got, _ := s.Screen("other"); got.DeviceKey != other.DeviceKey {
		t.Error("rotating one screen's key changed another's")
	}
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.DefaultScreen().DeviceKey != next {
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

func TestStateJSONIsValidAndReplayable(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := s.StateJSON()
	var st State
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&st); err != nil {
		t.Fatalf("the document this app emits is not one it will accept back: %v", err)
	}
	if err := s.ReplaceState(st); err != nil {
		t.Fatalf("round trip rejected: %v", err)
	}
}
