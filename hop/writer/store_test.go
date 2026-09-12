package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T, content string) (*store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "links.txt")
	if content != "" {
		os.WriteFile(path, []byte(content), 0o644)
	}
	return &store{path: path}, path
}

func TestStoreAddPreservesFile(t *testing.T) {
	s, path := tempStore(t, "# header\n\nlinkedin=https://linkedin.com/in/x\n")
	if err := s.Add("gh", "https://github.com/baely"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "# header\n\nlinkedin=https://linkedin.com/in/x\ngh=https://github.com/baely\n"
	if string(got) != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
	ls, err := s.List()
	if err != nil || len(ls) != 2 || ls[1].Path != "/gh" {
		t.Fatalf("list: %+v %v", ls, err)
	}
}

func TestStoreCreatesMissingFile(t *testing.T) {
	s, path := tempStore(t, "")
	if ls, err := s.List(); err != nil || len(ls) != 0 {
		t.Fatalf("missing file should list empty: %v %v", ls, err)
	}
	if err := s.Add("", "https://root.example"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "=https://root.example\n" {
		t.Fatalf("file: %q", got)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, want 0644 so hop (another uid) can read it", fi.Mode().Perm())
	}
}

func TestStoreUpdateAndDeleteKeepPlace(t *testing.T) {
	s, path := tempStore(t, "a=https://a.example\n# mid\nb=https://b.example\nc=https://c.example\n")
	if err := s.Update("b", "https://b2.example"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "# mid\nb=https://b2.example\nc=https://c.example\n"
	if string(got) != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
}

func TestStoreRejects(t *testing.T) {
	s, path := tempStore(t, "a=https://a.example\n")
	cases := []struct {
		name string
		err  error
	}{
		{"duplicate", s.Add("a", "https://x.example")},
		{"bad url", s.Add("b", "not a url")},
		{"bad key", s.Add("b c", "https://x.example")},
		{"leading slash", s.Add("/b", "https://x.example")},
		{"update missing", s.Update("zzz", "https://x.example")},
		{"delete missing", s.Delete("zzz")},
	}
	for _, c := range cases {
		if c.err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a=https://a.example\n" {
		t.Fatalf("file changed by rejected edits: %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestStoreIndexOfIgnoresGarbageLines(t *testing.T) {
	// A hand-edited file with a broken line still lets us find the good ones,
	// but writing it back is refused because hop could not load it.
	s, _ := tempStore(t, "a=https://a.example\nbroken line\n")
	err := s.Update("a", "https://a2.example")
	if err == nil || !strings.Contains(err.Error(), "expected key=url") {
		t.Fatalf("expected parse error from write, got %v", err)
	}
}
