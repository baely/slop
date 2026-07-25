package main

import (
	"strings"
	"testing"
)

// TestBitmapScalingIsPixelDoubling is the whole justification for the bitmap
// mask path: a 2x render must be the 1x render with every pixel doubled, which
// can only be true if no resampling filter is anywhere near a bitmap glyph.
func TestBitmapScalingIsPixelDoubling(t *testing.T) {
	const s = "Ag0 8% Wj|"
	for _, id := range []string{"pixel7x13", "pixel8x16", "pixel8x16-bold"} {
		tf := faceByID[id]
		one := typeset(id, tf.Native)
		two := typeset(id, tf.Native*2)
		if one.scale != 1 || two.scale != 2 {
			t.Fatalf("%s: scales are %d and %d, want 1 and 2", id, one.scale, two.scale)
		}

		w := one.Measure(s) + 4
		h := one.Ascent() + one.Descent() + 4
		c1 := newCanvas(w, h)
		c1.drawString(one, 2, 2+one.Ascent(), s, inkBlack)
		c2 := newCanvas(w*2, h*2)
		c2.drawString(two, 4, 4+two.Ascent(), s, inkBlack)

		ink := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				want := c1.g.Pix[y*c1.g.Stride+x]
				if want == inkBlack {
					ink++
				}
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						got := c2.g.Pix[(y*2+dy)*c2.g.Stride+(x*2+dx)]
						if got != want {
							t.Fatalf("%s: 2x pixel (%d,%d) = %#02x, 1x pixel (%d,%d) = %#02x; the scale is not a pure doubling",
								id, x*2+dx, y*2+dy, got, x, y, want)
						}
					}
				}
			}
		}
		if ink == 0 {
			t.Fatalf("%s: drew nothing at 1x", id)
		}
	}
}

// A bitmap face may only be drawn at whole multiples of its native size.
func TestBitmapRejectsNonIntegerScale(t *testing.T) {
	tf := faceByID["pixel8x16"]
	for _, size := range []int{9, 15, 17, 20, 24, 31, 33, 65, 100} {
		if tf.SizeOK(size) {
			t.Errorf("Pixel 8x16 accepted %dpx, which is not a multiple of 16", size)
		}
		advice := tf.SizeAdvice(size)
		if advice == "" {
			t.Fatalf("%dpx produced no advice", size)
		}
		// And whatever the renderer is handed, it snaps rather than smooths.
		ts := typeset(tf.ID, size)
		if ts.size%tf.Native != 0 {
			t.Errorf("typeset(%d) kept an illegal size %d", size, ts.size)
		}
		if ts.scale*tf.Native != ts.size {
			t.Errorf("typeset(%d): scale %d x native %d != size %d", size, ts.scale, tf.Native, ts.size)
		}
	}
	for _, size := range []int{16, 32, 48, 64} {
		if !tf.SizeOK(size) {
			t.Errorf("Pixel 8x16 refused its own legal size %d", size)
		}
	}
}

func TestSizeAdviceIsSpecific(t *testing.T) {
	tests := []struct {
		font  string
		size  int
		wants []string
	}{
		{"go", 11, []string{"Go Regular at 11px thins out on e-ink", "Try Pixel 8×16 at 16px."}},
		{"go-mono", 12, []string{"Go Mono at 12px thins out on e-ink", "Pixel 8×16 at 16px"}},
		{"go-bold", 13, []string{"Go Bold at 13px thins out on e-ink", "Pixel 8×16 Bold at 16px"}},
		{"pixel8x16", 20, []string{"no 20px size", "whole multiples of 16px", "16, 32, 48 or 64px"}},
		{"pixel8x16", 8, []string{"below its native 16px", "cannot be scaled down"}},
		{"pixel7x13", 100, []string{"stops at 52px"}},
		{"go", 500, []string{"Go Regular stops at 400px."}},
	}
	for _, tc := range tests {
		tf, ok := lookupFace(tc.font)
		if !ok {
			t.Fatalf("unknown face %q", tc.font)
		}
		got := tf.SizeAdvice(tc.size)
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s@%d advice = %q, missing %q", tc.font, tc.size, got, want)
			}
		}
	}

	// The legal pairings say nothing at all.
	for _, tf := range faces {
		if tf.Bitmap {
			for _, s := range tf.LegalSizes() {
				if a := tf.SizeAdvice(s); a != "" {
					t.Errorf("%s@%d should be fine, got %q", tf.ID, s, a)
				}
			}
			continue
		}
		for _, s := range []int{tf.Min, tf.Min + 4, 40, 120, tf.Max} {
			if a := tf.SizeAdvice(s); a != "" {
				t.Errorf("%s@%d should be fine, got %q", tf.ID, s, a)
			}
		}
	}
}

func TestSnapSize(t *testing.T) {
	px := faceByID["pixel8x16"]
	for in, want := range map[int]int{1: 16, 16: 16, 20: 16, 25: 32, 32: 32, 90: 64} {
		if got := px.SnapSize(in); got != want {
			t.Errorf("pixel8x16.SnapSize(%d) = %d, want %d", in, got, want)
		}
	}
	go1 := faceByID["go"]
	for in, want := range map[int]int{1: 16, 16: 16, 25: 25, 900: 400} {
		if got := go1.SnapSize(in); got != want {
			t.Errorf("go.SnapSize(%d) = %d, want %d", in, got, want)
		}
	}
}

// The alpha cutoff is derived from the greyscale threshold, not chosen twice.
func TestCutoffAndThresholdAgree(t *testing.T) {
	if 255-alphaCutoff >= threshold {
		t.Fatalf("a glyph pixel at the cutoff (%d coverage -> grey %d) would not become ink at threshold %d",
			alphaCutoff, 255-alphaCutoff, threshold)
	}
	if 255-(alphaCutoff-1) < threshold {
		t.Fatalf("a glyph pixel just under the cutoff would still become ink; the two constants have drifted apart")
	}
}

// Every face at every legal size must render pure black and white, inside the
// block, at a readable amount of ink.
func TestEveryFaceAtEveryLegalSize(t *testing.T) {
	for _, tf := range faces {
		sizes := tf.LegalSizes()
		if sizes == nil {
			sizes = []int{tf.Min, tf.Min + 6, 24, 48, 96}
		}
		for _, size := range sizes {
			sc := Screen{Name: "t", W: 640, H: 200, Blocks: []Block{{
				Type: BlockText, X: 24, Y: 20, W: 560, H: 160, Anchor: "nw", Align: "left",
				Text: "Bin Night 09:15", Font: tf.ID, Size: size,
			}}}
			img := Render(sc, paramsFor(sc), RenderCtx{Now: fixedNow})
			assertPureAndInside(t, img, sc, sc.Blocks[0], tf.ID+" at "+itoa(size))
		}
	}
}

// The outline faces really are refused below their measured floor, all the way
// through validation, with the message that names the bitmap alternative.
func TestOutlineFloorIsEnforcedByTheValidator(t *testing.T) {
	sc := Screen{Name: "t", W: 400, H: 200}
	b := Block{Type: BlockText, X: 0, Y: 0, W: 200, H: 40, Anchor: "nw", Align: "left",
		Text: "hello", Font: "go", Size: 11}
	err := b.Validate(2, sc, nil)
	if err == nil {
		t.Fatal("11px Go Regular passed validation")
	}
	got := err.Error()
	for _, want := range []string{"block 3 (text)", `"size"`, "Go Regular at 11px thins out on e-ink", "Pixel 8×16 at 16px"} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q is missing %q", got, want)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func TestWrapAndFit(t *testing.T) {
	ts := typeset("go", 20)
	lines := ts.wrap("the quick brown fox jumps over the lazy dog", 120)
	if len(lines) < 3 {
		t.Fatalf("expected several lines, got %v", lines)
	}
	for _, l := range lines {
		if w := ts.Measure(l); w > 120 {
			t.Errorf("line %q measures %d, over 120", l, w)
		}
	}
	// An unbreakable word is broken rather than allowed to overhang.
	long := ts.wrap(strings.Repeat("M", 60), 100)
	for _, l := range long {
		if w := ts.Measure(l); w > 100 {
			t.Errorf("long word line %q measures %d", l, w)
		}
	}
	// Explicit newlines always split.
	if got := ts.wrap("a\nb", 500); len(got) != 2 {
		t.Errorf("newline did not split: %v", got)
	}

	if got := ts.fit("short", 500); got != "short" {
		t.Errorf("short string mangled: %q", got)
	}
	if got := ts.fit("an extremely long label that will not fit", 90); ts.Measure(got) > 90 {
		t.Errorf("%q still measures %d", got, ts.Measure(got))
	}
	if ts.fit("anything", 0) != "" {
		t.Error("zero width should produce nothing")
	}
}

// The sample strip is capped so a 300px clock does not produce a 300px-tall
// form field; when it is capped the editor says so rather than quietly
// showing you a different size than the one you chose.
func TestSampleStripIsCapped(t *testing.T) {
	big := SampleStrip("go-bold", 300)
	if h := big.Bounds().Dy(); h > sampleCap+24 {
		t.Errorf("a 300px sample came out %dpx tall; the cap is %d", h, sampleCap)
	}
	small := SampleStrip("go", 16)
	if h := small.Bounds().Dy(); h > 40 {
		t.Errorf("a 16px sample came out %dpx tall", h)
	}
	if big.Bounds().Dx() > 900 || small.Bounds().Dx() > 900 {
		t.Error("a sample strip is wider than the 900px cap")
	}
}
