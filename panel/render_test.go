package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"net/url"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 7, 25, 14, 32, 11, 0, time.UTC)

func sampleScreen() Screen {
	sc := Screen{Name: "default", W: 800, H: 480, DeviceKey: "k", UpdatedAt: fixedNow}
	sc.Blocks = starterBlocks("clock-rows", sc)
	return sc
}

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

// assertPureAndInside is the assertion every block type has to pass: nothing
// but pure black and pure white, and no ink outside the block's own box.
func assertPureAndInside(t *testing.T, img *image.Paletted, sc Screen, b Block, what string) {
	t.Helper()
	if len(img.Palette) != 2 {
		t.Fatalf("%s: palette has %d entries", what, len(img.Palette))
	}
	bounds := img.Bounds()
	inkIn, inkOut := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			black := r == 0 && g == 0 && bl == 0 && a == 0xffff
			white := r == 0xffff && g == 0xffff && bl == 0xffff && a == 0xffff
			if !black && !white {
				t.Fatalf("%s: pixel (%d,%d) is neither pure black nor pure white: %d %d %d %d", what, x, y, r, g, bl, a)
			}
			if !black {
				continue
			}
			if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
				inkIn++
			} else {
				inkOut++
				if inkOut < 3 {
					t.Errorf("%s: ink at (%d,%d) is outside the block box (%d,%d %dx%d)", what, x, y, b.X, b.Y, b.W, b.H)
				}
			}
		}
	}
	if inkOut > 0 {
		t.Fatalf("%s: %d ink pixels escaped the block bounds", what, inkOut)
	}
	if inkIn == 0 {
		t.Fatalf("%s: the block drew nothing at all", what)
	}
}

// oneBlockScreen puts a single block on a plain canvas at a known offset.
func oneBlockScreen(b Block) (Screen, Block) {
	b.X, b.Y, b.W, b.H = 40, 50, 320, 180
	b = b.Normalize()
	return Screen{Name: "t", W: 480, H: 320, Blocks: []Block{b}}, b
}

func testCtx() RenderCtx {
	return RenderCtx{
		Now:   fixedNow,
		Data:  func(b Block) (string, bool) { return "21.4", false },
		Image: func(id string) (*image.Gray, bool) { return gradient(200, 140), id != "" },
	}
}

func gradient(w, h int) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			g.Pix[y*g.Stride+x] = uint8((x*255/w + y*255/h) / 2)
		}
	}
	return g
}

// TestEveryBlockTypeIsPureAndContained is the headline test: for each of the
// ten block types, the render is 1-bit and nothing leaks out of the box.
func TestEveryBlockTypeIsPureAndContained(t *testing.T) {
	cases := map[string]Block{
		BlockText: {Type: BlockText, Text: "Kitchen Panel", Font: "go-bold", Size: 32, Anchor: "c", Align: "center"},
		BlockClock: {Type: BlockClock, Format: "24h", Font: "go-bold", Size: 96, Anchor: "c",
			Align: "center"},
		BlockDate: {Type: BlockDate, Format: "long", Font: "go", Size: 24, Anchor: "c", Align: "center"},
		BlockRows: {Type: BlockRows, Font: "pixel8x16", Size: 16, Rules: true, Rows: []Row{{"Bin Night", "Tuesday"}, {"Battery", "3.94 V"}, {"Tram 96", "6 min"}}},
		BlockList: {Type: BlockList, Font: "pixel8x16", Size: 16, Bullets: true, Items: []string{"First", "Second", "Third"}},
		BlockDivider: {Type: BlockDivider, Orient: "horizontal", Thickness: 4,
			Anchor: "c"},
		BlockBox:      {Type: BlockBox, Thickness: 3},
		BlockProgress: {Type: BlockProgress, Label: "Battery", Value: 62, Font: "pixel8x16", Size: 16},
		BlockImage:    {Type: BlockImage, Image: "img1", Dither: "floyd", Fit: "cover", Anchor: "c"},
		BlockData: {Type: BlockData, URL: "https://example.com/a.json", Path: "v", Text: "{{v}}°C",
			Fallback: "—", Interval: 300, Timeout: 5, Font: "go-bold", Size: 40, Anchor: "c", Align: "center"},
	}
	for name, blk := range cases {
		t.Run(name, func(t *testing.T) {
			sc, b := oneBlockScreen(blk)
			img := Render(sc, paramsFor(sc), testCtx())
			assertPureAndInside(t, img, sc, b, name)
		})
	}

	// The variants that take a different drawing path.
	variants := map[string]Block{
		"box filled":       {Type: BlockBox, Fill: true},
		"divider vertical": {Type: BlockDivider, Orient: "vertical", Thickness: 6, Anchor: "c"},
		"text inverted":    {Type: BlockText, Text: "Header", Font: "go-bold", Size: 28, Invert: true},
		"text wrapped":     {Type: BlockText, Text: strings.Repeat("wrap this text ", 12), Font: "go", Size: 18, Wrap: true},
		"image atkinson":   {Type: BlockImage, Image: "img1", Dither: "atkinson", Fit: "contain", Anchor: "c"},
		"image threshold":  {Type: BlockImage, Image: "img1", Dither: "threshold", Fit: "stretch"},
		"image missing":    {Type: BlockImage, Image: "gone", Dither: "floyd", Fit: "contain"},
		"progress zero":    {Type: BlockProgress, Label: "Empty", Value: 0, Font: "pixel8x16", Size: 16},
		"progress full":    {Type: BlockProgress, Label: "Full", Value: 100, Font: "pixel8x16", Size: 16},
		"rows overflowing": {Type: BlockRows, Font: "pixel8x16", Size: 16, Rules: true, Rows: manyRows(24)},
		"list overflowing": {Type: BlockList, Font: "pixel8x16", Size: 16, Items: manyItems(24)},
	}
	for name, blk := range variants {
		t.Run(name, func(t *testing.T) {
			sc, b := oneBlockScreen(blk)
			ctx := testCtx()
			if name == "image missing" {
				ctx.Image = func(id string) (*image.Gray, bool) { return nil, false }
			}
			img := Render(sc, paramsFor(sc), ctx)
			assertPureAndInside(t, img, sc, b, name)
		})
	}
}

func manyRows(n int) []Row {
	out := make([]Row, n)
	for i := range out {
		out[i] = Row{Label: "Label " + itoa(i), Value: itoa(i*7) + " min"}
	}
	return out
}

func manyItems(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "Item number " + itoa(i)
	}
	return out
}

// TestPNGIsTrue1Bit checks the encoded bytes, not just the decoded pixels.
func TestPNGIsTrue1Bit(t *testing.T) {
	sc := sampleScreen()
	for _, p := range []Params{
		{W: 800, H: 480},
		{W: 400, H: 300},
		{W: 800, H: 480, Rotate: 90},
		{W: 800, H: 480, Invert: true},
	} {
		img := Render(sc, p, testCtx())
		b, err := EncodePNG(img)
		if err != nil {
			t.Fatalf("%v: %v", p, err)
		}
		c := chunks(t, b)
		ihdr, ok := c["IHDR"]
		if !ok || len(ihdr) != 13 {
			t.Fatalf("%v: bad IHDR", p)
		}
		if w := binary.BigEndian.Uint32(ihdr[0:4]); int(w) != p.OutW() {
			t.Errorf("%v: IHDR width = %d, want %d", p, w, p.OutW())
		}
		if h := binary.BigEndian.Uint32(ihdr[4:8]); int(h) != p.OutH() {
			t.Errorf("%v: IHDR height = %d, want %d", p, h, p.OutH())
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

func TestNoGreyPixelsOnAFullScreen(t *testing.T) {
	sc := sampleScreen()
	for _, p := range []Params{{W: 800, H: 480}, {W: 400, H: 300}, {W: 800, H: 480, Rotate: 270}, {W: 300, H: 500, Invert: true}} {
		b, err := EncodePNG(Render(sc, p, testCtx()))
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
		bounds := pal.Bounds()
		if bounds.Dx() != p.OutW() || bounds.Dy() != p.OutH() {
			t.Fatalf("%v: bounds = %v", p, bounds)
		}
		black, white := 0, 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, bl, a := pal.At(x, y).RGBA()
				switch {
				case r == 0 && g == 0 && bl == 0 && a == 0xffff:
					black++
				case r == 0xffff && g == 0xffff && bl == 0xffff && a == 0xffff:
					white++
				default:
					t.Fatalf("%v: pixel (%d,%d) is neither black nor white", p, x, y)
				}
			}
		}
		if black == 0 || white == 0 {
			t.Errorf("%v: image is a single colour (%d black, %d white)", p, black, white)
		}
	}
}

func TestBinLengthExact(t *testing.T) {
	sc := sampleScreen()
	sizes := [][2]int{{800, 480}, {400, 300}, {101, 77}, {64, 64}, {999, 65}}
	for _, s := range sizes {
		for _, rot := range []int{0, 90, 180, 270} {
			p := Params{W: s[0], H: s[1], Rotate: rot}
			want := ((p.OutW() + 7) / 8) * p.OutH()
			if got := p.BinLen(); got != want {
				t.Errorf("%v: BinLen = %d, want %d", p, got, want)
			}
			got := len(PackBits(Render(sc, p, testCtx())))
			if got != want {
				t.Errorf("%v: packed %d bytes, want %d", p, got, want)
			}
		}
	}
}

func TestPackingIsMSBFirst(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 12, 2), panelPalette)
	for i := range img.Pix {
		img.Pix[i] = idxBlack
	}
	img.SetColorIndex(0, 0, idxWhite)
	img.SetColorIndex(7, 0, idxWhite)
	img.SetColorIndex(8, 0, idxWhite)
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

func TestDeterministicWithinMinute(t *testing.T) {
	sc := sampleScreen()
	p := paramsFor(sc)
	ctx := testCtx()
	a, err := EncodePNG(Render(sc, p, ctx))
	if err != nil {
		t.Fatal(err)
	}
	ctx.Now = fixedNow.Add(48 * time.Second)
	b, err := EncodePNG(Render(sc, p, ctx))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("same minute produced different bytes")
	}
	ctx.Now = fixedNow.Add(time.Minute)
	c, err := EncodePNG(Render(sc, p, ctx))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, c) {
		t.Error("a new minute produced identical bytes; the clock is not rendering")
	}
}

// A seconds clock changes the whole screen's granularity, deliberately.
func TestSecondsFormatChangesGranularity(t *testing.T) {
	sc := Screen{Name: "t", W: 300, H: 100, Blocks: []Block{
		{Type: BlockClock, X: 0, Y: 0, W: 300, H: 100, Format: "24h", Font: "go-bold", Size: 40},
	}}
	if g := sc.Granularity(); g != time.Minute {
		t.Errorf("plain clock granularity = %v", g)
	}
	sc.Blocks[0].Format = "24h-sec"
	if g := sc.Granularity(); g != time.Second {
		t.Errorf("seconds clock granularity = %v", g)
	}
	ctx := testCtx()
	a := PackBits(Render(sc, paramsFor(sc), ctx))
	ctx.Now = fixedNow.Add(time.Second)
	b := PackBits(Render(sc, paramsFor(sc), ctx))
	if bytes.Equal(a, b) {
		t.Error("a seconds clock did not change after one second")
	}
}

func TestRotationSwapsOutputBounds(t *testing.T) {
	sc := sampleScreen()
	for _, rot := range []int{0, 180} {
		got := Render(sc, Params{W: 800, H: 480, Rotate: rot}, testCtx()).Bounds()
		if got.Dx() != 800 || got.Dy() != 480 {
			t.Errorf("rotate=%d: bounds %v, want 800x480", rot, got)
		}
	}
	for _, rot := range []int{90, 270} {
		got := Render(sc, Params{W: 800, H: 480, Rotate: rot}, testCtx()).Bounds()
		if got.Dx() != 480 || got.Dy() != 800 {
			t.Errorf("rotate=%d: bounds %v, want 480x800", rot, got)
		}
	}
	a := Render(sc, Params{W: 400, H: 300}, testCtx())
	b := Render(sc, Params{W: 400, H: 300, Rotate: 180}, testCtx())
	if bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("rotate=180 changed nothing")
	}
	if a.Pix[0] != b.Pix[len(b.Pix)-1] {
		t.Error("rotate=180 did not mirror the corners")
	}
}

func TestInvertIsComplement(t *testing.T) {
	sc := sampleScreen()
	a := Render(sc, Params{W: 400, H: 300}, testCtx())
	b := Render(sc, Params{W: 400, H: 300, Invert: true}, testCtx())
	for i := range a.Pix {
		if a.Pix[i] == b.Pix[i] {
			t.Fatalf("pixel %d not inverted", i)
		}
	}
}

func TestRenderSurvivesAwkwardContent(t *testing.T) {
	long := strings.Repeat("wide", 200)
	sc := Screen{Name: "t", W: 800, H: 480}
	sc.Blocks = []Block{
		{Type: BlockText, X: 0, Y: 0, W: 800, H: 40, Text: long, Font: "go", Size: 24},
		{Type: BlockRows, X: 0, Y: 40, W: 800, H: 200, Font: "pixel7x13", Size: 13, Rows: manyRows(maxRows)},
		{Type: BlockList, X: 0, Y: 240, W: 800, H: 20, Font: "pixel7x13", Size: 13, Items: manyItems(maxListItems)},
		{Type: BlockProgress, X: 0, Y: 270, W: 20, H: 4, Value: 50, Font: "pixel7x13", Size: 13},
		{Type: BlockBox, X: 700, Y: 400, W: 4, H: 4, Thickness: 40},
	}
	for _, p := range []Params{{W: 64, H: 64}, {W: 800, H: 480}, {W: 2400, H: 1200}} {
		img := Render(sc, p, testCtx())
		if img.Bounds().Dx() != p.OutW() {
			t.Fatalf("%v: wrong width", p)
		}
		for _, v := range img.Pix {
			if v != idxBlack && v != idxWhite {
				t.Fatalf("%v: palette index %d escaped", p, v)
			}
		}
	}
	// And a screen with nothing on it.
	img := Render(Screen{Name: "t", W: 200, H: 100}, Params{W: 200, H: 100}, testCtx())
	if img.Bounds().Dy() != 100 {
		t.Fatal("an empty screen broke the render")
	}
}

func TestParseParams(t *testing.T) {
	sc := Screen{Name: "t", W: 800, H: 480}
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
			got, err := ParseParams(q, sc)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got none", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
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

	// A screen's own settings are the starting point.
	rot := Screen{Name: "t", W: 480, H: 800, Rotate: 90, Invert: true}
	got, err := ParseParams(url.Values{}, rot)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Params{W: 480, H: 800, Rotate: 90, Invert: true}) {
		t.Fatalf("screen settings not used: %+v", got)
	}
	if got.OutW() != 800 || got.OutH() != 480 {
		t.Fatalf("rotated output = %dx%d, want 800x480", got.OutW(), got.OutH())
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

func TestEncodeDecodeRoundTrip(t *testing.T) {
	sc := sampleScreen()
	img := Render(sc, Params{W: 200, H: 120}, testCtx())
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

// The canvas may hold greys; the PNG must not.
func TestThresholdIsTheOnlyExit(t *testing.T) {
	c := newCanvas(60, 30)
	c.drawString(typeset("go", 20), 2, 20, "Ag", inkBlack)
	grey := 0
	for _, v := range c.g.Pix {
		if v != inkBlack && v != inkWhite {
			grey++
		}
	}
	if grey == 0 {
		t.Skip("no antialiasing produced; nothing to prove")
	}
	img := toPaletted(c.g, false)
	for _, v := range img.Pix {
		if v != idxBlack && v != idxWhite {
			t.Fatalf("palette index %d escaped the threshold", v)
		}
	}
}

// Blocks draw in list order, so a later one covers an earlier one.
func TestLaterBlocksDrawOverEarlier(t *testing.T) {
	sc := Screen{Name: "t", W: 100, H: 100, Blocks: []Block{
		{Type: BlockBox, X: 0, Y: 0, W: 100, H: 100, Fill: true},
		{Type: BlockBox, X: 20, Y: 20, W: 60, H: 60, Fill: false, Thickness: 60},
	}}
	// The second box is an outline thick enough to be solid, drawn in black
	// over black: still black. Make it meaningful by inverting it instead.
	sc.Blocks[1] = Block{Type: BlockText, X: 20, Y: 20, W: 60, H: 60,
		Text: "X", Font: "go-bold", Size: 40, Anchor: "c", Align: "center", Invert: true}
	img := Render(sc, paramsFor(sc), testCtx())
	// Inside the second block there must now be white pixels; outside it, none.
	whiteInside, whiteOutside := 0, 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			if img.ColorIndexAt(x, y) != idxWhite {
				continue
			}
			if x >= 20 && x < 80 && y >= 20 && y < 80 {
				whiteInside++
			} else {
				whiteOutside++
			}
		}
	}
	if whiteInside == 0 {
		t.Error("the later block did not draw over the filled box")
	}
	if whiteOutside != 0 {
		t.Errorf("%d white pixels outside the later block", whiteOutside)
	}
}

func TestSampleStripIsPure(t *testing.T) {
	for _, tf := range faces {
		size := tf.Min
		if tf.Bitmap {
			size = tf.Native
		}
		img := SampleStrip(tf.ID, size)
		black := 0
		for _, v := range img.Pix {
			if v != idxBlack && v != idxWhite {
				t.Fatalf("%s: sample strip has index %d", tf.ID, v)
			}
			if v == idxBlack {
				black++
			}
		}
		if black == 0 {
			t.Errorf("%s: sample strip is blank", tf.ID)
		}
	}
}
