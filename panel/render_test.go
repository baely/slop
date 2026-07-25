package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"net/url"
	"testing"
	"time"
)

func sampleConfig() Config {
	return Config{
		Title:  "Kitchen Panel",
		Footer: "study panel, esp32",
		Rows: []Row{
			{"Bin Night", "Tuesday"},
			{"Next Tram 96", "6 min"},
			{"Melbourne", "14C, rain"},
			{"Standup", "09:15"},
			{"Battery", "3.94 V"},
			{"Uptime", "41 days"},
		},
	}
}

var fixedNow = time.Date(2026, 7, 25, 14, 32, 11, 0, time.UTC)

// chunks walks the PNG chunk stream so the encoded header can be inspected
// rather than trusted.
func chunks(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	const sig = "\x89PNG\r\n\x1a\n"
	if !bytes.HasPrefix(b, []byte(sig)) {
		t.Fatalf("not a PNG: % x", b[:8])
	}
	out := map[string][]byte{}
	p := len(sig)
	for p+8 <= len(b) {
		n := int(binary.BigEndian.Uint32(b[p : p+4]))
		typ := string(b[p+4 : p+8])
		if p+8+n+4 > len(b) {
			t.Fatalf("truncated chunk %s", typ)
		}
		out[typ] = b[p+8 : p+8+n]
		p += 8 + n + 4
		if typ == "IEND" {
			break
		}
	}
	return out
}

// TestPNGIsTrue1Bit checks the encoded bytes, not just the decoded pixels: bit
// depth 1, colour type 3 (paletted), and a two-entry PLTE of pure black and
// pure white. A 24-bit image of black and white pixels would fail here.
func TestPNGIsTrue1Bit(t *testing.T) {
	for _, p := range []Params{
		{W: 800, H: 480},
		{W: 400, H: 300},
		{W: 1404, H: 1872},
		{W: 800, H: 480, Rotate: 90},
		{W: 800, H: 480, Invert: true},
	} {
		img := Render(sampleConfig(), fixedNow, p)
		b, err := EncodePNG(img)
		if err != nil {
			t.Fatalf("%v: %v", p, err)
		}
		c := chunks(t, b)
		ihdr, ok := c["IHDR"]
		if !ok || len(ihdr) != 13 {
			t.Fatalf("%v: bad IHDR", p)
		}
		if w := binary.BigEndian.Uint32(ihdr[0:4]); int(w) != p.W {
			t.Errorf("%v: IHDR width = %d", p, w)
		}
		if h := binary.BigEndian.Uint32(ihdr[4:8]); int(h) != p.H {
			t.Errorf("%v: IHDR height = %d", p, h)
		}
		if depth := ihdr[8]; depth != 1 {
			t.Errorf("%v: bit depth = %d, want 1", p, depth)
		}
		if ct := ihdr[9]; ct != 3 {
			t.Errorf("%v: colour type = %d, want 3 (paletted)", p, ct)
		}
		plte, ok := c["PLTE"]
		if !ok {
			t.Fatalf("%v: no PLTE chunk", p)
		}
		if want := []byte{0, 0, 0, 0xff, 0xff, 0xff}; !bytes.Equal(plte, want) {
			t.Errorf("%v: PLTE = % x, want % x", p, plte, want)
		}
		if _, ok := c["tRNS"]; ok {
			t.Errorf("%v: unexpected tRNS chunk", p)
		}
	}
}

// TestNoGreyPixels decodes the encoded PNG and asserts every single pixel is
// pure black or pure white. Anti-aliasing anywhere in the pipeline fails this.
func TestNoGreyPixels(t *testing.T) {
	for _, p := range []Params{
		{W: 800, H: 480},
		{W: 400, H: 300},
		{W: 1404, H: 1872},
		{W: 800, H: 480, Rotate: 270},
		{W: 300, H: 500, Invert: true},
	} {
		b, err := EncodePNG(Render(sampleConfig(), fixedNow, p))
		if err != nil {
			t.Fatal(err)
		}
		img, format, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if format != "png" {
			t.Fatalf("format = %s", format)
		}
		pal, ok := img.(*image.Paletted)
		if !ok {
			t.Fatalf("%v: decoded as %T, want *image.Paletted", p, img)
		}
		if len(pal.Palette) != 2 {
			t.Fatalf("%v: palette has %d entries", p, len(pal.Palette))
		}
		bounds := img.Bounds()
		if bounds.Dx() != p.W || bounds.Dy() != p.H {
			t.Fatalf("%v: bounds = %v", p, bounds)
		}
		black, white := 0, 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, bl, a := img.At(x, y).RGBA()
				switch {
				case r == 0 && g == 0 && bl == 0 && a == 0xffff:
					black++
				case r == 0xffff && g == 0xffff && bl == 0xffff && a == 0xffff:
					white++
				default:
					t.Fatalf("%v: pixel (%d,%d) is neither black nor white: %d %d %d %d", p, x, y, r, g, bl, a)
				}
			}
		}
		if black == 0 || white == 0 {
			t.Errorf("%v: image is a single colour (%d black, %d white)", p, black, white)
		}
	}
}

// TestBinLengthExact: ceil(w/8)*h, for every preset and every rotation.
func TestBinLengthExact(t *testing.T) {
	sizes := [][2]int{{800, 480}, {400, 300}, {1404, 1872}, {101, 77}, {64, 64}, {999, 65}}
	for _, s := range sizes {
		for _, rot := range []int{0, 90, 180, 270} {
			p := Params{W: s[0], H: s[1], Rotate: rot}
			want := ((p.W + 7) / 8) * p.H
			if got := p.BinLen(); got != want {
				t.Errorf("%v: BinLen = %d, want %d", p, got, want)
			}
			got := len(PackBits(Render(sampleConfig(), fixedNow, p)))
			if got != want {
				t.Errorf("%v: packed %d bytes, want %d", p, got, want)
			}
		}
	}
}

// TestPackingIsMSBFirst pins the documented bit order against a hand-built row.
func TestPackingIsMSBFirst(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 12, 2), panelPalette)
	for i := range img.Pix {
		img.Pix[i] = idxBlack
	}
	// Row 0: white at x=0 and x=7 -> 0b10000001 = 0x81, then x=8 -> 0x80.
	img.SetColorIndex(0, 0, idxWhite)
	img.SetColorIndex(7, 0, idxWhite)
	img.SetColorIndex(8, 0, idxWhite)
	// Row 1: white at x=3 -> 0b00010000 = 0x10.
	img.SetColorIndex(3, 1, idxWhite)

	got := PackBits(img)
	want := []byte{0x81, 0x80, 0x10, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("packed % x, want % x", got, want)
	}
	if len(got) != ((12+7)/8)*2 {
		t.Fatalf("length %d", len(got))
	}
}

// TestDeterministicWithinMinute is what makes the ETag meaningful.
func TestDeterministicWithinMinute(t *testing.T) {
	cfg := sampleConfig()
	p := Params{W: 800, H: 480}
	a, err := EncodePNG(Render(cfg, fixedNow, p))
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodePNG(Render(cfg, fixedNow.Add(48*time.Second), p))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("same minute produced different bytes")
	}
	c, err := EncodePNG(Render(cfg, fixedNow.Add(time.Minute), p))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, c) {
		t.Error("a new minute produced identical bytes; the clock is not rendering")
	}
	cfg.Rows[0].Value = "Wednesday"
	d, err := EncodePNG(Render(cfg, fixedNow, p))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, d) {
		t.Error("changed content produced identical bytes")
	}
}

func TestRotationKeepsRequestedBounds(t *testing.T) {
	for _, rot := range []int{0, 90, 180, 270} {
		p := Params{W: 800, H: 480, Rotate: rot}
		got := Render(sampleConfig(), fixedNow, p).Bounds()
		if got.Dx() != 800 || got.Dy() != 480 {
			t.Errorf("rotate=%d: bounds %v, want 800x480", rot, got)
		}
	}
	// 180 is its own inverse.
	a := Render(sampleConfig(), fixedNow, Params{W: 400, H: 300})
	b := Render(sampleConfig(), fixedNow, Params{W: 400, H: 300, Rotate: 180})
	if bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("rotate=180 changed nothing")
	}
	if a.Pix[0] != b.Pix[len(b.Pix)-1] {
		t.Error("rotate=180 did not mirror the corners")
	}
}

func TestInvertIsComplement(t *testing.T) {
	a := Render(sampleConfig(), fixedNow, Params{W: 400, H: 300})
	b := Render(sampleConfig(), fixedNow, Params{W: 400, H: 300, Invert: true})
	for i := range a.Pix {
		if a.Pix[i] == b.Pix[i] {
			t.Fatalf("pixel %d not inverted", i)
		}
	}
}

func TestRenderSurvivesAwkwardContent(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "wide"
	}
	cfg := Config{Title: long, Footer: long, Rows: make([]Row, maxRows)}
	for i := range cfg.Rows {
		cfg.Rows[i] = Row{Label: long, Value: long}
	}
	for _, p := range []Params{{W: 64, H: 64}, {W: 800, H: 480}, {W: 2400, H: 1200}} {
		img := Render(cfg, fixedNow, p)
		if img.Bounds().Dx() != p.W {
			t.Fatalf("%v: wrong width", p)
		}
	}
	// And the empty case.
	img := Render(Config{Title: "panel"}, fixedNow, Params{W: 800, H: 480})
	if img.Bounds().Dy() != 480 {
		t.Fatal("empty config broke the render")
	}
}

func TestParseParams(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    Params
		wantErr string
	}{
		{"defaults", "", Params{W: 800, H: 480}, ""},
		{"explicit", "w=400&h=300", Params{W: 400, H: 300}, ""},
		{"preset", "preset=1404x1872", Params{W: 1404, H: 1872}, ""},
		{"preset then override", "preset=400x300&w=500", Params{W: 500, H: 300}, ""},
		{"rotate", "rotate=270", Params{W: 800, H: 480, Rotate: 270}, ""},
		{"invert word", "invert=true", Params{W: 800, H: 480, Invert: true}, ""},
		{"invert off", "invert=0", Params{W: 800, H: 480}, ""},
		{"unknown preset", "preset=nope", Params{}, "unknown preset"},
		{"w not a number", "w=big", Params{}, "whole number"},
		{"w too small", "w=8", Params{}, "between 64 and 2400"},
		{"w too big", "w=99999", Params{}, "between 64 and 2400"},
		{"area too big", "w=2400&h=2400", Params{}, "the cap is"},
		{"bad rotate", "rotate=45", Params{}, "0, 90, 180 or 270"},
		{"bad invert", "invert=maybe", Params{}, "invert must be"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseParams(q)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got none", tc.wantErr)
				}
				if !bytes.Contains([]byte(err.Error()), []byte(tc.wantErr)) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParamsQueryIsStable(t *testing.T) {
	p := Params{W: 800, H: 480, Rotate: 90, Invert: true}
	first := p.Query()
	for i := 0; i < 20; i++ {
		if p.Query() != first {
			t.Fatal("Query() is not stable; the render cache key would thrash")
		}
	}
}

func TestFitTruncates(t *testing.T) {
	f := face(fRegular, 18)
	long := "an extremely long label that will not fit in ninety pixels at all"
	got := fit(f, long, 90)
	if got == long {
		t.Fatal("expected truncation")
	}
	if measure(f, got) > 90 {
		t.Fatalf("%q still measures %d px", got, measure(f, got))
	}
	if short := fit(f, "ok", 200); short != "ok" {
		t.Fatalf("short string was mangled: %q", short)
	}
	if fit(f, "anything", 0) != "" {
		t.Fatal("zero width should produce nothing")
	}
}

// The scratch canvas may hold greys; the PNG must not. This asserts the
// threshold step is the only path from one to the other.
func TestThresholdIsTheOnlyExit(t *testing.T) {
	c := newCanvas(60, 30)
	c.text(face(fRegular, 20), 2, 20, "Ag", inkBlack)
	grey := 0
	for _, v := range c.g.Pix {
		if v != inkBlack && v != inkWhite {
			grey++
		}
	}
	if grey == 0 {
		t.Skip("no antialiasing produced; nothing to prove")
	}
	img := Render(Config{Title: "Ag"}, fixedNow, Params{W: 200, H: 100})
	for _, v := range img.Pix {
		if v != idxBlack && v != idxWhite {
			t.Fatalf("palette index %d escaped the threshold", v)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	img := Render(sampleConfig(), fixedNow, Params{W: 200, H: 120})
	b, err := EncodePNG(img)
	if err != nil {
		t.Fatal(err)
	}
	back, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	pal, ok := back.(*image.Paletted)
	if !ok {
		t.Fatalf("decoded %T", back)
	}
	if !bytes.Equal(PackBits(img), PackBits(pal)) {
		t.Error("packing the decoded image differs from packing the source")
	}
}
