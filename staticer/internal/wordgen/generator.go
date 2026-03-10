package wordgen

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// Generator generates random subdomain names
type Generator struct {
	adjectives []string
	nouns      []string
}

// NewGenerator creates a new subdomain generator
func NewGenerator() *Generator {
	return &Generator{
		adjectives: adjectives,
		nouns:      nouns,
	}
}

// Generate generates a random subdomain
func (g *Generator) Generate() string {
	adj := g.adjectives[randomInt(len(g.adjectives))]
	noun := g.nouns[randomInt(len(g.nouns))]
	return fmt.Sprintf("%s-%s", adj, noun)
}

// GenerateUnique generates a unique subdomain
// existsFunc is called to check if a subdomain already exists
func (g *Generator) GenerateUnique(existsFunc func(string) bool) (string, error) {
	for i := 0; i < 100; i++ {
		subdomain := g.Generate()
		if !existsFunc(subdomain) {
			return subdomain, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique subdomain after 100 attempts")
}

// randomInt returns a cryptographically secure random integer in [0, max)
func randomInt(max int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		// Fallback to first element if random fails (should never happen)
		return 0
	}
	return int(n.Int64())
}
