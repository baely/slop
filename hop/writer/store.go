package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/baely/slop/hop/links"
)

// store edits the links file in place, line by line, so comments and order
// survive. Every write is validated with the same parser hop uses and then
// swapped in atomically, so hop never sees a half-written or invalid file.
type store struct {
	path string
	mu   sync.Mutex
}

// List returns the current links. A missing file is an empty list.
func (s *store) List() ([]links.Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, err := s.read()
	if err != nil {
		return nil, err
	}
	return links.Parse(strings.NewReader(strings.Join(lines, "\n")))
}

// Add appends key=url. The key is validated as hop would and must be new.
func (s *store) Add(key, url string) error {
	path, err := links.KeyPath(key)
	if err != nil {
		return err
	}
	if err := links.ValidateTarget(url); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, err := s.read()
	if err != nil {
		return err
	}
	if i := indexOf(lines, path); i >= 0 {
		return fmt.Errorf("%s already exists", display(path))
	}
	return s.write(append(lines, key+"="+url))
}

// Update changes the URL of an existing link, keeping its line where it is.
func (s *store) Update(key, url string) error {
	path, err := links.KeyPath(key)
	if err != nil {
		return err
	}
	if err := links.ValidateTarget(url); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, err := s.read()
	if err != nil {
		return err
	}
	i := indexOf(lines, path)
	if i < 0 {
		return fmt.Errorf("%s does not exist", display(path))
	}
	lines[i] = key + "=" + url
	return s.write(lines)
}

// Delete removes a link's line.
func (s *store) Delete(key string) error {
	path, err := links.KeyPath(key)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, err := s.read()
	if err != nil {
		return err
	}
	i := indexOf(lines, path)
	if i < 0 {
		return fmt.Errorf("%s does not exist", display(path))
	}
	return s.write(append(lines[:i], lines[i+1:]...))
}

// read returns the file's lines without terminators. Missing file: none.
func (s *store) read() ([]string, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(b), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// write validates the new content and replaces the file atomically.
func (s *store) write(lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	if _, err := links.Parse(strings.NewReader(content)); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".links-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// indexOf finds the line defining path, or -1.
func indexOf(lines []string, path string) int {
	for i, line := range lines {
		if l, ok, err := links.ParseLine(line); ok && err == nil && l.Path == path {
			return i
		}
	}
	return -1
}

func display(path string) string { return path }
