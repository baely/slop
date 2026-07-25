package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestValidatorNamesTheProblem is the promise the JSON escape hatch makes: no
// generic "invalid layout", ever. Every failure names the block, its type and
// the field.
func TestValidatorNamesTheProblem(t *testing.T) {
	sc := Screen{Name: "kitchen", W: 800, H: 480}

	tests := []struct {
		name  string
		block Block
		index int
		wants []string
	}{
		{
			name:  "unknown block type",
			block: Block{Type: "sparkline", X: 0, Y: 0, W: 100, H: 20},
			index: 2,
			wants: []string{"block 3", `unknown block type "sparkline"`, "known types:", "progress", "data"},
		},
		{
			name:  "missing block type",
			block: Block{X: 0, Y: 0, W: 100, H: 20},
			index: 0,
			wants: []string{"block 1", `"type"`, "is missing", "known types:"},
		},
		{
			name:  "negative width",
			block: Block{Type: BlockBox, X: 10, Y: 10, W: -40, H: 20, Thickness: 2},
			index: 1,
			wants: []string{"block 2 (box)", `"w"`, "at least 1 pixel", "-40"},
		},
		{
			name:  "negative height",
			block: Block{Type: BlockBox, X: 10, Y: 10, W: 40, H: -2, Thickness: 2},
			index: 0,
			wants: []string{"block 1 (box)", `"h"`, "at least 1 pixel", "-2"},
		},
		{
			name:  "negative x",
			block: Block{Type: BlockBox, X: -5, Y: 10, W: 40, H: 20, Thickness: 2},
			index: 0,
			wants: []string{"block 1 (box)", `"x"`, "must not be negative", "-5"},
		},
		{
			name:  "off the right edge",
			block: Block{Type: BlockBox, X: 700, Y: 10, W: 200, H: 20, Thickness: 2},
			index: 3,
			wants: []string{"block 4 (box)", `"x"`, "700", `"w"`, "200", "900", "past the right edge", "800px wide"},
		},
		{
			name:  "off the bottom",
			block: Block{Type: BlockBox, X: 10, Y: 400, W: 100, H: 200, Thickness: 2},
			index: 0,
			wants: []string{"block 1 (box)", `"y"`, "600", "past the bottom", "480px tall"},
		},
		{
			name:  "text with no text",
			block: Block{Type: BlockText, X: 0, Y: 0, W: 100, H: 30, Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (text)", `"text"`, "is required", "must not be blank"},
		},
		{
			name:  "image with no image",
			block: Block{Type: BlockImage, X: 0, Y: 0, W: 100, H: 100, Dither: "floyd", Fit: "contain"},
			index: 5,
			wants: []string{"block 6 (image)", `"image"`, "is required", "upload an image"},
		},
		{
			name:  "image referring to nothing",
			block: Block{Type: BlockImage, X: 0, Y: 0, W: 100, H: 100, Image: "ghost", Dither: "floyd", Fit: "contain"},
			index: 0,
			wants: []string{"block 1 (image)", `"image"`, `"ghost"`, "not an uploaded image"},
		},
		{
			name:  "data with no url",
			block: Block{Type: BlockData, X: 0, Y: 0, W: 100, H: 30, Path: "a.b", Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (data)", `"url"`, "is required"},
		},
		{
			name:  "data with no path",
			block: Block{Type: BlockData, X: 0, Y: 0, W: 100, H: 30, URL: "https://x.test/a.json", Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (data)", `"path"`, "is required", "current.temp_c"},
		},
		{
			name:  "data with a file url",
			block: Block{Type: BlockData, X: 0, Y: 0, W: 100, H: 30, URL: "file:///etc/passwd", Path: "a", Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (data)", `"url"`, "must be http or https", `"file"`},
		},
		{
			name: "data template without the placeholder",
			block: Block{Type: BlockData, X: 0, Y: 0, W: 100, H: 30, URL: "https://x.test/a.json",
				Path: "a", Text: "just words", Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (data)", `"text"`, "must contain {{v}}"},
		},
		{
			name: "data interval too short",
			block: Block{Type: BlockData, X: 0, Y: 0, W: 100, H: 30, URL: "https://x.test/a.json",
				Path: "a", Interval: 5, Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (data)", `"interval"`, "between 30 and 86400 seconds", "got 5"},
		},
		{
			name:  "bad anchor",
			block: Block{Type: BlockBox, X: 0, Y: 0, W: 10, H: 10, Anchor: "middle", Thickness: 1},
			index: 0,
			wants: []string{"block 1 (box)", `"anchor"`, "must be one of", `"middle"`},
		},
		{
			name:  "bad clock format",
			block: Block{Type: BlockClock, X: 0, Y: 0, W: 100, H: 40, Format: "%H:%M", Font: "go", Size: 24},
			index: 0,
			wants: []string{"block 1 (clock)", `"format"`, "24h, 24h-sec, 12h", `"%H:%M"`},
		},
		{
			name:  "unknown font",
			block: Block{Type: BlockText, X: 0, Y: 0, W: 100, H: 40, Text: "hi", Font: "helvetica", Size: 20},
			index: 0,
			wants: []string{"block 1 (text)", `"font"`, "not a known face", `"helvetica"`, "pixel8x16"},
		},
		{
			name:  "outline font too small",
			block: Block{Type: BlockText, X: 0, Y: 0, W: 100, H: 40, Text: "hi", Font: "go-mono", Size: 9},
			index: 0,
			wants: []string{"block 1 (text)", `"size"`, "Go Mono at 9px thins out on e-ink", "Pixel 8×16 at 16px"},
		},
		{
			name:  "bitmap font at a non-multiple",
			block: Block{Type: BlockText, X: 0, Y: 0, W: 100, H: 40, Text: "hi", Font: "pixel7x13", Size: 20},
			index: 0,
			wants: []string{"block 1 (text)", `"size"`, "no 20px size", "whole multiples of 13px", "13, 26, 39 or 52px"},
		},
		{
			name:  "progress out of range",
			block: Block{Type: BlockProgress, X: 0, Y: 0, W: 100, H: 40, Value: 140, Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (progress)", `"value"`, "between 0 and 100", "140"},
		},
		{
			name:  "divider with no thickness",
			block: Block{Type: BlockDivider, X: 0, Y: 0, W: 100, H: 4, Orient: "horizontal", Thickness: -1},
			index: 0,
			wants: []string{"block 1 (divider)", `"thickness"`, "between 1 and 64", "-1"},
		},
		{
			name:  "rows with no rows",
			block: Block{Type: BlockRows, X: 0, Y: 0, W: 100, H: 40, Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (rows)", `"rows"`, "is required", "label/value"},
		},
		{
			name:  "list with no items",
			block: Block{Type: BlockList, X: 0, Y: 0, W: 100, H: 40, Font: "go", Size: 20},
			index: 0,
			wants: []string{"block 1 (list)", `"items"`, "is required", "at least one line"},
		},
	}

	imageIDs := map[string]bool{"real": true}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.block.Normalize().Validate(tc.index, sc, imageIDs)
			if err == nil {
				t.Fatalf("no error; the block was accepted")
			}
			got := err.Error()
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Errorf("message %q is missing %q", got, want)
				}
			}
		})
	}
}

func TestValidatorAcceptsGoodBlocks(t *testing.T) {
	sc := Screen{Name: "t", W: 800, H: 480}
	ids := map[string]bool{"real": true}
	good := []Block{
		{Type: BlockText, X: 0, Y: 0, W: 200, H: 40, Text: "hi", Font: "go", Size: 20},
		{Type: BlockClock, X: 0, Y: 40, W: 200, H: 100, Format: "12h-sec", Font: "go-bold", Size: 64},
		{Type: BlockDate, X: 0, Y: 140, W: 200, H: 30, Format: "iso", Font: "pixel8x16", Size: 16},
		{Type: BlockRows, X: 0, Y: 180, W: 400, H: 100, Font: "pixel7x13", Size: 26, Rows: []Row{{"a", "b"}}},
		{Type: BlockList, X: 400, Y: 180, W: 300, H: 100, Font: "pixel8x16-bold", Size: 32, Items: []string{"x"}},
		{Type: BlockDivider, X: 0, Y: 300, W: 800, H: 3, Orient: "horizontal", Thickness: 3},
		{Type: BlockBox, X: 0, Y: 310, W: 100, H: 60, Fill: true},
		{Type: BlockProgress, X: 120, Y: 310, W: 300, H: 40, Value: 0, Font: "go", Size: 16},
		{Type: BlockImage, X: 500, Y: 310, W: 200, H: 150, Image: "real", Dither: "atkinson", Fit: "cover"},
		{Type: BlockData, X: 0, Y: 400, W: 300, H: 60, URL: "http://example.test/a.json", Path: "a.0.b",
			Text: "{{v}} kWh", Interval: 60, Timeout: 3, Font: "go-mono", Size: 24},
	}
	for i, b := range good {
		if err := b.Normalize().Validate(i, sc, ids); err != nil {
			t.Errorf("block %d rejected: %v", i, err)
		}
	}
	if err := validateScreen(Screen{Name: "t", W: 800, H: 480, Blocks: good}, ids); err != nil {
		t.Errorf("whole screen rejected: %v", err)
	}
}

func TestScreenValidation(t *testing.T) {
	tests := []struct {
		name string
		sc   Screen
		want string
	}{
		{"bad name", Screen{Name: "Kitchen Panel!", W: 800, H: 480}, "is not valid"},
		{"empty name", Screen{Name: "", W: 800, H: 480}, "is not valid"},
		{"tiny", Screen{Name: "t", W: 10, H: 480}, "width must be between 64 and 2400"},
		{"huge", Screen{Name: "t", W: 2400, H: 2400}, "the cap is"},
		{"bad rotate", Screen{Name: "t", W: 800, H: 480, Rotate: 45}, "rotate must be 0, 90, 180 or 270"},
	}
	for _, tc := range tests {
		err := validateScreen(tc.sc, nil)
		if err == nil {
			t.Errorf("%s: accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %q missing %q", tc.name, err, tc.want)
		}
	}
	if err := validateScreen(Screen{Name: "t", W: 800, H: 480, Blocks: make([]Block, maxBlocks+1)}, nil); err == nil ||
		!strings.Contains(err.Error(), "the cap is") {
		t.Errorf("block cap not enforced: %v", err)
	}
}

// Normalize fills in what was omitted without silently correcting what was set.
func TestNormalizeDefaults(t *testing.T) {
	b := Block{Type: BlockClock, W: 100, H: 40}.Normalize()
	if b.Format != "24h" || b.Font != "go-bold" || b.Size != 96 || b.Anchor != "nw" || b.Align != "left" {
		t.Fatalf("clock defaults not applied: %+v", b)
	}
	b = Block{Type: BlockData, W: 100, H: 40, URL: " https://x.test/a ", Path: " a.b "}.Normalize()
	if b.Text != "{{v}}" || b.Fallback != "—" || b.Interval != defaultInterval || b.Timeout != defaultTimeout {
		t.Fatalf("data defaults not applied: %+v", b)
	}
	if b.URL != "https://x.test/a" || b.Path != "a.b" {
		t.Fatalf("url/path not trimmed: %q %q", b.URL, b.Path)
	}
	// An explicit wrong value is left alone so the validator can name it.
	b = Block{Type: BlockText, W: 10, H: 10, Text: "x", Font: "go", Size: 3}.Normalize()
	if b.Size != 3 {
		t.Fatalf("Normalize silently corrected an illegal size to %d", b.Size)
	}
}

func TestCleanMultiKeepsNewlines(t *testing.T) {
	tests := map[string]string{
		"a\nb":       "a\nb",
		"a\r\nb":     "a\nb",
		"a\x00b":     "ab",
		"  a  \n b ": "a\n b",
		"\n\na\n\n":  "a",
	}
	for in, want := range tests {
		if got := cleanMulti(in, 100); got != want {
			t.Errorf("cleanMulti(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanMulti(strings.Repeat("x", 900), 20); len([]rune(got)) != 20 {
		t.Errorf("cap not applied: %d runes", len([]rune(got)))
	}
}

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"Kitchen Panel": "kitchen-panel",
		"  hallway  ":   "hallway",
		"a__b":          "a-b",
		"!!!":           "",
		"Study/Desk":    "studydesk",
		"-lead-":        "lead",
	}
	for in, want := range tests {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// Round-tripping through JSON must not lose or invent anything.
func TestBlockJSONRoundTrip(t *testing.T) {
	orig := Block{Type: BlockData, X: 1, Y: 2, W: 3, H: 4, Anchor: "se", Text: "{{v}}%",
		Font: "go-mono", Size: 24, Align: "right", Wrap: true, Invert: true,
		URL: "https://x.test/a", Path: "a.b", Interval: 60, Timeout: 3, Fallback: "?"}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back Block
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, orig) {
		t.Fatalf("round trip changed the block:\n%+v\n%+v", orig, back)
	}
	// Irrelevant fields stay out of the document.
	if strings.Contains(string(b), "\"rows\"") || strings.Contains(string(b), "\"dither\"") {
		t.Errorf("unused fields leaked into the JSON: %s", b)
	}
}

func TestStartersAreValid(t *testing.T) {
	for _, st := range starters {
		for _, dim := range [][2]int{{800, 480}, {400, 300}, {1404, 1872}, {240, 160}} {
			sc := Screen{Name: "t", W: dim[0], H: dim[1]}
			sc.Blocks = starterBlocks(st.ID, sc)
			if len(sc.Blocks) == 0 {
				t.Fatalf("%s at %dx%d produced no blocks", st.ID, dim[0], dim[1])
			}
			ids := map[string]bool{}
			for i := range sc.Blocks {
				if sc.Blocks[i].Type == BlockImage {
					sc.Blocks[i].Image = "real"
					ids["real"] = true
				}
			}
			if err := validateScreen(sc, ids); err != nil {
				t.Errorf("starter %s at %dx%d is invalid: %v", st.ID, dim[0], dim[1], err)
			}
		}
	}
}
