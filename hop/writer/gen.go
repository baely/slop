package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// rules describe the random keys to mint.
type rules struct {
	Length      int
	Lower       bool // a-z
	Upper       bool // A-Z
	Digits      bool // 0-9
	Unambiguous bool // drop 0 O 1 l I, which read alike in most fonts
}

const (
	maxLength = 32
	ambiguous = "0O1lI"
)

var defaultRules = rules{Length: 6, Lower: true, Digits: true, Unambiguous: true}

// alphabet is the set of characters the rules allow.
func (r rules) alphabet() (string, error) {
	if r.Length < 1 || r.Length > maxLength {
		return "", fmt.Errorf("length must be between 1 and %d", maxLength)
	}
	var sb strings.Builder
	if r.Lower {
		sb.WriteString("abcdefghijklmnopqrstuvwxyz")
	}
	if r.Upper {
		sb.WriteString("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	}
	if r.Digits {
		sb.WriteString("0123456789")
	}
	a := sb.String()
	if r.Unambiguous {
		a = strings.Map(func(c rune) rune {
			if strings.ContainsRune(ambiguous, c) {
				return -1
			}
			return c
		}, a)
	}
	if a == "" {
		return "", fmt.Errorf("pick at least one character class")
	}
	return a, nil
}

// generate mints a key the rules allow that taken does not already know.
func generate(r rules, taken func(string) bool) (string, error) {
	a, err := r.alphabet()
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 100; attempt++ {
		b := make([]byte, r.Length)
		for i := range b {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(a))))
			if err != nil {
				return "", err
			}
			b[i] = a[n.Int64()]
		}
		if k := string(b); !taken(k) {
			return k, nil
		}
	}
	return "", fmt.Errorf("could not find a free key of length %d; raise the length", r.Length)
}
