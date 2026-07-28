package ingest

import (
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	moneyRe = regexp.MustCompile(`(?i)(?:AUD|USD|NZD|A\$|\$)\s*([0-9]{1,3}(?:,[0-9]{3})+(?:\.[0-9]{1,2})?|[0-9]+(?:\.[0-9]{1,2})?)`)
	totalRe = regexp.MustCompile(`(?i)\b(grand\s+total|total|amount\s+due|amount\s+paid|amount|paid|charged|payment)\b`)

	scriptRe = regexp.MustCompile(`(?is)<script.*?</script>`)
	styleRe  = regexp.MustCompile(`(?is)<style.*?</style>`)
	// Block-level closers become newlines so "line" context survives; table
	// cells (</td>) stay on one line so a Total label keeps its value.
	brRe  = regexp.MustCompile(`(?i)<(?:br|/p|/div|/tr|/h[1-6]|/li)[^>]*>`)
	tagRe = regexp.MustCompile(`<[^>]+>`)
)

// extractAmount guesses the receipt total in cents. Amounts on lines that
// mention total/amount/paid win (largest of them, since grand totals include
// tax); otherwise the largest amount anywhere. Purely a first guess — the UI
// lets you correct it.
func extractAmount(subject, text string) (int64, bool) {
	var best, bestTotal int64 = -1, -1
	scan := func(line string, isTotal bool) {
		for _, m := range moneyRe.FindAllStringSubmatch(line, -1) {
			c, ok := parseCents(m[1])
			if !ok {
				continue
			}
			if c > best {
				best = c
			}
			if isTotal && c > bestTotal {
				bestTotal = c
			}
		}
	}

	scan(subject, totalRe.MatchString(subject))
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if len(line) > 500 {
			line = line[:500]
		}
		isTotal := totalRe.MatchString(line)
		scan(line, isTotal)
		// Label and value often land on adjacent lines (divs, table rows).
		if isTotal && i+1 < len(lines) {
			next := lines[i+1]
			if len(next) > 500 {
				next = next[:500]
			}
			scan(next, true)
		}
	}

	if bestTotal > 0 {
		return bestTotal, true
	}
	if best > 0 {
		return best, true
	}
	return 0, false
}

func parseCents(s string) (int64, bool) {
	s = strings.ReplaceAll(s, ",", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 || f >= 1e6 {
		return 0, false
	}
	return int64(math.Round(f * 100)), true
}

func stripHTML(s string) string {
	s = scriptRe.ReplaceAllString(s, " ")
	s = styleRe.ReplaceAllString(s, " ")
	s = brRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, " ")
	return html.UnescapeString(s)
}
