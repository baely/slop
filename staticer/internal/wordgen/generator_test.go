package wordgen

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	gen := NewGenerator()

	// Generate a subdomain
	subdomain := gen.Generate()

	// Check format (adjective-noun)
	parts := strings.Split(subdomain, "-")
	if len(parts) != 2 {
		t.Errorf("Expected format 'adjective-noun', got %s", subdomain)
	}

	// Check both parts are non-empty
	if parts[0] == "" || parts[1] == "" {
		t.Errorf("Empty word in subdomain: %s", subdomain)
	}
}

func TestGenerateUnique(t *testing.T) {
	gen := NewGenerator()

	// Mock function that says first subdomain exists
	existingSubdomains := map[string]bool{
		"happy-tree": true,
	}

	existsFunc := func(subdomain string) bool {
		return existingSubdomains[subdomain]
	}

	// Generate unique subdomain (might take a few tries)
	subdomain, err := gen.GenerateUnique(existsFunc)
	if err != nil {
		t.Fatalf("Failed to generate unique subdomain: %v", err)
	}

	// Should not be in existing set
	if existingSubdomains[subdomain] {
		t.Errorf("Generated subdomain %s already exists", subdomain)
	}
}

func TestGenerateUniqueness(t *testing.T) {
	gen := NewGenerator()

	// Generate multiple subdomains and check they're different
	generated := make(map[string]bool)
	duplicates := 0

	for i := 0; i < 100; i++ {
		subdomain := gen.Generate()
		if generated[subdomain] {
			duplicates++
		}
		generated[subdomain] = true
	}

	// Some duplicates are expected due to randomness, but not too many
	if duplicates > 10 {
		t.Errorf("Too many duplicates: %d out of 100", duplicates)
	}
}

func TestGenerateUniqueFailure(t *testing.T) {
	gen := NewGenerator()

	// Function that always returns true (everything exists)
	alwaysExists := func(subdomain string) bool {
		return true
	}

	// Should fail after max attempts
	_, err := gen.GenerateUnique(alwaysExists)
	if err == nil {
		t.Error("Expected error when all subdomains exist, got nil")
	}
}

func TestWordLists(t *testing.T) {
	if len(adjectives) == 0 {
		t.Error("Adjectives list is empty")
	}

	if len(nouns) == 0 {
		t.Error("Nouns list is empty")
	}

	// Check for no empty strings
	for _, adj := range adjectives {
		if adj == "" {
			t.Error("Empty adjective in list")
		}
	}

	for _, noun := range nouns {
		if noun == "" {
			t.Error("Empty noun in list")
		}
	}
}
