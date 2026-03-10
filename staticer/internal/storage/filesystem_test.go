package storage

import (
	"archive/zip"
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractZIP(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fs := newFilesystem(tempDir, logger)

	// Create a test ZIP in memory
	zipData := createTestZIP(t)

	// Extract ZIP
	result, err := fs.ExtractZIP("test-site", zipData, 1000, 10*1024*1024)
	if err != nil {
		t.Fatalf("Failed to extract ZIP: %v", err)
	}

	// Verify result
	if result.FileCount != 2 {
		t.Errorf("Expected 2 files, got %d", result.FileCount)
	}

	// Verify files exist
	siteDir := filepath.Join(tempDir, "test-site")

	indexPath := filepath.Join(siteDir, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Error("index.html not extracted")
	}

	stylePath := filepath.Join(siteDir, "style.css")
	if _, err := os.Stat(stylePath); os.IsNotExist(err) {
		t.Error("style.css not extracted")
	}
}

func TestExtractZIPWithoutIndexHTML(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fs := newFilesystem(tempDir, logger)

	// Create ZIP without index.html
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	writer, _ := zipWriter.Create("other.html")
	writer.Write([]byte("<html></html>"))

	zipWriter.Close()

	// Should fail validation
	_, err := fs.ExtractZIP("test-site", buf.Bytes(), 1000, 10*1024*1024)
	if err == nil {
		t.Error("Expected error for ZIP without index.html")
	}

	if !strings.Contains(err.Error(), "index.html") {
		t.Errorf("Expected error about index.html, got: %v", err)
	}
}

func TestExtractZIPPathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fs := newFilesystem(tempDir, logger)

	// Create ZIP with path traversal attempt
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Add index.html
	writer, _ := zipWriter.Create("index.html")
	writer.Write([]byte("<html></html>"))

	// Add malicious file
	writer, _ = zipWriter.Create("../../../etc/passwd")
	writer.Write([]byte("malicious"))

	zipWriter.Close()

	// Should fail validation
	_, err := fs.ExtractZIP("test-site", buf.Bytes(), 1000, 10*1024*1024)
	if err == nil {
		t.Error("Expected error for path traversal attempt")
	}

	if !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("Expected path traversal error, got: %v", err)
	}
}

func TestExtractZIPTooManyFiles(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fs := newFilesystem(tempDir, logger)

	// Create ZIP with too many files
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	writer, _ := zipWriter.Create("index.html")
	writer.Write([]byte("<html></html>"))

	// Add many files
	for i := 0; i < 15; i++ {
		writer, _ := zipWriter.Create("file" + string(rune(i)) + ".txt")
		writer.Write([]byte("content"))
	}

	zipWriter.Close()

	// Should fail with max files = 10
	_, err := fs.ExtractZIP("test-site", buf.Bytes(), 10, 10*1024*1024)
	if err == nil {
		t.Error("Expected error for too many files")
	}

	if !strings.Contains(err.Error(), "too many files") {
		t.Errorf("Expected too many files error, got: %v", err)
	}
}

func TestDeleteSite(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fs := newFilesystem(tempDir, logger)

	// Create a site directory
	siteDir := filepath.Join(tempDir, "test-site")
	os.MkdirAll(siteDir, 0755)

	// Add a file
	os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("<html></html>"), 0644)

	// Delete site
	err := fs.DeleteSite("test-site")
	if err != nil {
		t.Fatalf("Failed to delete site: %v", err)
	}

	// Verify directory is gone
	if _, err := os.Stat(siteDir); !os.IsNotExist(err) {
		t.Error("Site directory still exists after deletion")
	}
}

func TestDeleteNonExistentSite(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fs := newFilesystem(tempDir, logger)

	// Try to delete non-existent site
	err := fs.DeleteSite("does-not-exist")
	if err == nil {
		t.Error("Expected error when deleting non-existent site")
	}
}

func TestExtractZIPUnwrapNestedFolders(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	fs := newFilesystem(tempDir, logger)

	// Create ZIP with multiple levels of nesting: outer/inner/index.html
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	writer, _ := zipWriter.Create("outer/inner/index.html")
	writer.Write([]byte("<html><body>Nested</body></html>"))

	writer, _ = zipWriter.Create("outer/inner/style.css")
	writer.Write([]byte("body { color: blue; }"))

	zipWriter.Close()

	result, err := fs.ExtractZIP("nested-site", buf.Bytes(), 1000, 10*1024*1024)
	if err != nil {
		t.Fatalf("Failed to extract ZIP: %v", err)
	}

	if result.FileCount != 2 {
		t.Errorf("Expected 2 files, got %d", result.FileCount)
	}

	// Verify files are at root, not nested
	siteDir := filepath.Join(tempDir, "nested-site")

	indexPath := filepath.Join(siteDir, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Error("index.html should be at root after unwrapping nested folders")
	}

	stylePath := filepath.Join(siteDir, "style.css")
	if _, err := os.Stat(stylePath); os.IsNotExist(err) {
		t.Error("style.css should be at root after unwrapping nested folders")
	}

	// Verify nested paths don't exist
	nestedPath := filepath.Join(siteDir, "outer")
	if _, err := os.Stat(nestedPath); !os.IsNotExist(err) {
		t.Error("outer/ directory should not exist after unwrapping")
	}
}

// Helper function to create a test ZIP
func createTestZIP(t *testing.T) []byte {
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Add index.html
	writer, err := zipWriter.Create("index.html")
	if err != nil {
		t.Fatalf("Failed to create index.html in ZIP: %v", err)
	}
	writer.Write([]byte("<html><body>Test</body></html>"))

	// Add style.css
	writer, err = zipWriter.Create("style.css")
	if err != nil {
		t.Fatalf("Failed to create style.css in ZIP: %v", err)
	}
	writer.Write([]byte("body { color: red; }"))

	if err := zipWriter.Close(); err != nil {
		t.Fatalf("Failed to close ZIP writer: %v", err)
	}

	return buf.Bytes()
}
