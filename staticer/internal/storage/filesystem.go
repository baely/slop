package storage

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// filesystem handles file operations
type filesystem struct {
	sitesDir string
	logger   *slog.Logger
}

// newFilesystem creates a new filesystem handler
func newFilesystem(sitesDir string, logger *slog.Logger) *filesystem {
	return &filesystem{
		sitesDir: sitesDir,
		logger:   logger,
	}
}

// ExtractResult contains information about the extracted files
type ExtractResult struct {
	FileCount int
	TotalSize int64
}

// ExtractZIP extracts a ZIP file to the site directory
func (f *filesystem) ExtractZIP(subdomain string, zipData []byte, maxFiles int, maxSize int64) (*ExtractResult, error) {
	// Create ZIP reader from bytes
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("invalid ZIP file: %w", err)
	}

	// Validate before extraction
	if err := f.validateZIP(reader, maxFiles, maxSize); err != nil {
		return nil, err
	}

	// Create site directory
	siteDir := filepath.Join(f.sitesDir, subdomain)
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create site directory: %w", err)
	}

	// Extract files
	var fileCount int
	var totalSize int64

	for _, file := range reader.File {
		if err := f.extractFile(file, siteDir); err != nil {
			// Clean up on error
			os.RemoveAll(siteDir)
			return nil, fmt.Errorf("failed to extract file %s: %w", file.Name, err)
		}
		fileCount++
		totalSize += int64(file.UncompressedSize64)
	}

	// Unwrap single-folder ZIPs (common pattern from GitHub, zip tools, etc.)
	// Loop to handle multiple levels of nesting (e.g. outer/inner/index.html)
	for {
		unwrapped, err := f.unwrapSingleFolder(siteDir)
		if err != nil {
			f.logger.Warn("Failed to unwrap single folder", "error", err)
			break // Non-fatal - continue with nested structure
		}
		if !unwrapped {
			break
		}
	}

	f.logger.Info("ZIP extracted", "subdomain", subdomain, "files", fileCount, "size", totalSize)

	return &ExtractResult{
		FileCount: fileCount,
		TotalSize: totalSize,
	}, nil
}

// unwrapSingleFolder checks if the site directory contains only a single subdirectory
// and no files. If so, it moves the contents of that subdirectory up one level.
// This handles the common case where ZIPs contain a wrapper folder.
func (f *filesystem) unwrapSingleFolder(siteDir string) (bool, error) {
	entries, err := os.ReadDir(siteDir)
	if err != nil {
		return false, err
	}

	// Count directories and files at root level
	var dirs []os.DirEntry
	var files []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}

	// Only unwrap if there's exactly 1 directory and 0 files
	if len(dirs) != 1 || len(files) != 0 {
		return false, nil // Nothing to unwrap
	}

	wrapperDir := filepath.Join(siteDir, dirs[0].Name())
	f.logger.Info("Unwrapping single-folder ZIP", "folder", dirs[0].Name())

	// Get all entries from the wrapper directory
	wrapperEntries, err := os.ReadDir(wrapperDir)
	if err != nil {
		return false, err
	}

	// Move each entry from wrapper to parent
	for _, entry := range wrapperEntries {
		oldPath := filepath.Join(wrapperDir, entry.Name())
		newPath := filepath.Join(siteDir, entry.Name())

		if err := os.Rename(oldPath, newPath); err != nil {
			return false, fmt.Errorf("failed to move %s: %w", entry.Name(), err)
		}
	}

	// Remove the now-empty wrapper directory
	if err := os.Remove(wrapperDir); err != nil {
		return false, fmt.Errorf("failed to remove wrapper directory: %w", err)
	}

	return true, nil
}

// validateZIP validates the ZIP file before extraction
func (f *filesystem) validateZIP(reader *zip.Reader, maxFiles int, maxSize int64) error {
	var totalSize int64
	var hasIndexHTML bool

	if len(reader.File) > maxFiles {
		return fmt.Errorf("ZIP contains too many files (max %d)", maxFiles)
	}

	for _, file := range reader.File {
		// Check for index.html
		if file.Name == "index.html" || strings.HasSuffix(file.Name, "/index.html") {
			hasIndexHTML = true
		}

		// Check for path traversal
		if strings.Contains(file.Name, "..") {
			return fmt.Errorf("invalid file path: %s (path traversal detected)", file.Name)
		}

		// Check for absolute paths
		if filepath.IsAbs(file.Name) {
			return fmt.Errorf("invalid file path: %s (absolute paths not allowed)", file.Name)
		}

		// Accumulate size
		totalSize += int64(file.UncompressedSize64)
	}

	if !hasIndexHTML {
		return fmt.Errorf("ZIP must contain index.html")
	}

	if totalSize > maxSize {
		return fmt.Errorf("extracted size %d exceeds maximum %d (possible ZIP bomb)", totalSize, maxSize)
	}

	return nil
}

// extractFile extracts a single file from the ZIP
func (f *filesystem) extractFile(file *zip.File, destDir string) error {
	// Clean the file path
	cleanPath := filepath.Clean(file.Name)
	destPath := filepath.Join(destDir, cleanPath)

	// Ensure the destination is within destDir (extra safety)
	if !strings.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("invalid file path: %s", file.Name)
	}

	// Handle directories
	if file.FileInfo().IsDir() {
		return os.MkdirAll(destPath, file.Mode())
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Open source file
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	// Create destination file
	dest, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer dest.Close()

	// Copy contents
	_, err = io.Copy(dest, src)
	return err
}

// SaveSingleFile saves a single HTML file to the site directory
func (f *filesystem) SaveSingleFile(subdomain string, filename string, data []byte) error {
	// Create site directory
	siteDir := filepath.Join(f.sitesDir, subdomain)
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		return fmt.Errorf("failed to create site directory: %w", err)
	}

	// Always save as index.html for easy serving at root
	destPath := filepath.Join(siteDir, "index.html")

	// Write file
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		// Clean up on error
		os.RemoveAll(siteDir)
		return fmt.Errorf("failed to write file: %w", err)
	}

	f.logger.Info("Single file saved", "subdomain", subdomain, "original", filename, "size", len(data))
	return nil
}

// DeleteSite removes the site directory
func (f *filesystem) DeleteSite(subdomain string) error {
	siteDir := filepath.Join(f.sitesDir, subdomain)

	// Check if directory exists
	if _, err := os.Stat(siteDir); os.IsNotExist(err) {
		return fmt.Errorf("site directory not found: %s", subdomain)
	}

	// Remove directory and all contents
	if err := os.RemoveAll(siteDir); err != nil {
		return fmt.Errorf("failed to delete site directory: %w", err)
	}

	f.logger.Info("Site directory deleted", "subdomain", subdomain)
	return nil
}

// GetSitePath returns the absolute path to a site's directory
func (f *filesystem) GetSitePath(subdomain string) string {
	return filepath.Join(f.sitesDir, subdomain)
}
