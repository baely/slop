package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// ---------------------------------------------------------------------------
// Type on a 1-bit panel
//
// The output has no antialiasing, so font and size are not independent
// choices and this file refuses to pretend otherwise.
//
//   - A BITMAP face is pixel-perfect at its native size. It is legal ONLY at
//     that size and whole multiples of it, drawn by replicating each pixel
//     scale x scale — nearest neighbour, no smoothing, no resampling. A 1.6x
//     bitmap face is not a smaller compromise, it is broken glyphs.
//   - An OUTLINE face is rasterised with antialiasing and then thresholded.
//     Below a certain size the counters of a/e/o fill in and the whole line
//     turns to porridge. Each outline face carries the minimum size it
//     actually survives, measured (see alphaCutoff) rather than guessed.
//
// alphaCutoff is the glyph coverage level at which a pixel becomes ink.
// Chosen by measurement, not by eye: for each candidate cutoff, every glyph of
// Go Regular / Bold / Mono was rendered at 10-26px, thresholded, and checked
// for (a) enclosed counters surviving in a e o g p b d q 0 6 8 9 B D O P R and
// (b) total ink area against the true coverage area.
//
//	cutoff 96  keeps every counter down to 12px but lays down 20-30% more ink
//	           than the glyph actually covers: everything comes out fattened.
//	cutoff 128 tracks the true area well but starts eating counters below 16px
//	           and drops below 1.0 area retention, i.e. it thins stems.
//	cutoff 116 holds area retention at 1.00-1.12 across the whole legal range
//	           and keeps 17/17 counters from 15px up. Erring a hair toward
//	           keeping ink is the right direction: a stem you thresholded away
//	           is gone, a stem one pixel too fat is merely a bit bold.
//
// So: 116. The greyscale threshold below is derived from it, not chosen
// separately — text is composed black-on-white, so grey = 255 - coverage.
const alphaCutoff = 116

// threshold: greyscale values at or above this become white.
const threshold = 255 - alphaCutoff + 1 // 140

// typeface is one entry in the curated font list. The size rules live in the
// data so the editor, the validator and the renderer cannot disagree.
type typeface struct {
	ID     string
	Name   string
	Bitmap bool

	// Bitmap faces: Native is the 1x size; legal sizes are Native*1..Native*Steps.
	Native int
	Steps  int

	// Outline faces: the measured floor and a practical ceiling.
	Min int
	Max int

	// Alt is the bitmap face to suggest when an outline face is asked to go
	// too small — the advice a person can act on.
	Alt     string
	AltSize int

	Note string

	ttf []byte
	bmp *basicfont.Face
}

// faces is the whole menu. Nothing outside this list can be selected.
var faces = []typeface{
	{
		ID: "pixel7x13", Name: "Pixel 7×13", Bitmap: true, Native: 13, Steps: 4,
		Note: "Bitmap. Dense label/value rows and small print.", bmp: basicfont.Face7x13,
	},
	{
		ID: "pixel8x16", Name: "Pixel 8×16", Bitmap: true, Native: 16, Steps: 4,
		Note: "Bitmap, monospaced. The default for lists and values.", bmp: inconsolata.Regular8x16,
	},
	{
		ID: "pixel8x16-bold", Name: "Pixel 8×16 Bold", Bitmap: true, Native: 16, Steps: 4,
		Note: "Bitmap, monospaced, heavier. Headings on small panels.", bmp: inconsolata.Bold8x16,
	},
	{
		ID: "go", Name: "Go Regular", Min: 16, Max: 400, Alt: "pixel8x16", AltSize: 16,
		Note: "Outline. Body text at 16px and up.", ttf: goregular.TTF,
	},
	{
		ID: "go-bold", Name: "Go Bold", Min: 14, Max: 400, Alt: "pixel8x16-bold", AltSize: 16,
		Note: "Outline. Clocks, titles, anything large.", ttf: gobold.TTF,
	},
	{
		ID: "go-mono", Name: "Go Mono", Min: 16, Max: 400, Alt: "pixel8x16", AltSize: 16,
		Note: "Outline, monospaced. Values at 16px and up.", ttf: gomono.TTF,
	},
	{
		ID: "go-mono-bold", Name: "Go Mono Bold", Min: 16, Max: 400, Alt: "pixel8x16-bold", AltSize: 16,
		Note: "Outline, monospaced, heavier. Big numbers.", ttf: gomonobold.TTF,
	},
}

var faceByID = func() map[string]typeface {
	m := make(map[string]typeface, len(faces))
	for _, f := range faces {
		m[f.ID] = f
	}
	return m
}()

const defaultFontID = "pixel8x16"

func lookupFace(id string) (typeface, bool) {
	tf, ok := faceByID[strings.TrimSpace(id)]
	return tf, ok
}

// LegalSizes lists the sizes a bitmap face may be drawn at. Outline faces
// return nil: any integer inside [Min, Max] is legal for them.
func (t typeface) LegalSizes() []int {
	if !t.Bitmap {
		return nil
	}
	out := make([]int, 0, t.Steps)
	for i := 1; i <= t.Steps; i++ {
		out = append(out, t.Native*i)
	}
	return out
}

func (t typeface) SizeRange() (int, int) {
	if t.Bitmap {
		return t.Native, t.Native * t.Steps
	}
	return t.Min, t.Max
}

// SizeOK reports whether this face may be drawn at this size at all.
func (t typeface) SizeOK(size int) bool { return t.SizeAdvice(size) == "" }

// SizeAdvice returns "" when the pairing is fine, or a sentence naming the
// specific problem and the specific thing to do instead. It is used by the
// validator, shown live in the editor, and is deliberately not a generic
// "invalid value".
func (t typeface) SizeAdvice(size int) string {
	if t.Bitmap {
		if size < t.Native {
			return fmt.Sprintf("%s is a bitmap face: %dpx is below its native %dpx and it cannot be scaled down. Use %s.",
				t.Name, size, t.Native, joinSizes(t.LegalSizes()))
		}
		if size > t.Native*t.Steps {
			return fmt.Sprintf("%s stops at %dpx. Use %s, or an outline face like Go Bold for anything larger.",
				t.Name, t.Native*t.Steps, joinSizes(t.LegalSizes()))
		}
		if size%t.Native != 0 {
			return fmt.Sprintf("%s has no %dpx size: a bitmap face only scales in whole multiples of %dpx. Use %s.",
				t.Name, size, t.Native, joinSizes(t.LegalSizes()))
		}
		return ""
	}
	if size < t.Min {
		alt, _ := lookupFace(t.Alt)
		return fmt.Sprintf("%s at %dpx thins out on e-ink. Try %s at %dpx.", t.Name, size, alt.Name, t.AltSize)
	}
	if size > t.Max {
		return fmt.Sprintf("%s stops at %dpx.", t.Name, t.Max)
	}
	return ""
}

func joinSizes(sizes []int) string {
	parts := make([]string, len(sizes))
	for i, s := range sizes {
		parts[i] = fmt.Sprintf("%d", s)
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0] + "px"
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1] + "px"
	}
}

// SnapSize moves a size onto the nearest legal one for this face. Used when
// the font is switched underneath a size that is no longer valid, so the
// editor never silently keeps an illegal pairing.
func (t typeface) SnapSize(size int) int {
	if !t.Bitmap {
		if size < t.Min {
			return t.Min
		}
		if size > t.Max {
			return t.Max
		}
		return size
	}
	legal := t.LegalSizes()
	best := legal[0]
	for _, s := range legal {
		if abs(s-size) < abs(best-size) {
			best = s
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------------------------------------------------------------------------
// typesetter: one face at one size, ready to measure and draw.

type typesetter struct {
	tf    typeface
	size  int
	scale int // whole-pixel replication factor; always 1 for outline faces
	face  font.Face
}

var (
	parsedOnce sync.Once
	parsed     map[string]*sfnt.Font
	parseErr   error

	faceMu    sync.Mutex
	faceCache = map[faceKey]font.Face{}
)

type faceKey struct {
	id   string
	size int
}

func parseFonts() error {
	parsedOnce.Do(func() {
		parsed = map[string]*sfnt.Font{}
		for _, tf := range faces {
			if tf.ttf == nil {
				continue
			}
			f, err := opentype.Parse(tf.ttf)
			if err != nil {
				parseErr = err
				return
			}
			parsed[tf.ID] = f
		}
	})
	return parseErr
}

// typeset resolves a (font, size) pair into something drawable. An illegal
// pairing is snapped rather than refused here: validation has already had its
// say at save time, and a render must never fail closed on a live panel.
func typeset(id string, size int) typesetter {
	tf, ok := lookupFace(id)
	if !ok {
		tf = faceByID[defaultFontID]
	}
	if !tf.SizeOK(size) {
		size = tf.SnapSize(size)
	}
	if tf.Bitmap {
		return typesetter{tf: tf, size: size, scale: size / tf.Native, face: tf.bmp}
	}
	return typesetter{tf: tf, size: size, scale: 1, face: outlineFace(tf, size)}
}

// outlineFace returns a cached face. Sizes are in pixels (DPI pinned to 72 so
// point size == pixel size). Full hinting snaps stems to the pixel grid, which
// is worth a great deal once the render is thresholded to 1 bit.
func outlineFace(tf typeface, size int) font.Face {
	key := faceKey{tf.ID, size}
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f
	}
	if err := parseFonts(); err != nil {
		faceCache[key] = basicfont.Face7x13
		return basicfont.Face7x13
	}
	f, err := opentype.NewFace(parsed[tf.ID], &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		f = basicfont.Face7x13
	}
	if len(faceCache) > 256 {
		faceCache = map[faceKey]font.Face{}
	}
	faceCache[key] = f
	return f
}

func (t typesetter) Ascent() int  { return t.face.Metrics().Ascent.Ceil() * t.scale }
func (t typesetter) Descent() int { return t.face.Metrics().Descent.Ceil() * t.scale }

// LineHeight is the vertical advance between two lines of this face.
func (t typesetter) LineHeight() int {
	if t.tf.Bitmap {
		return t.size
	}
	h := t.face.Metrics().Height.Ceil()
	if h <= 0 {
		h = t.Ascent() + t.Descent()
	}
	return h
}

func (t typesetter) Measure(s string) int {
	return font.MeasureString(t.face, s).Ceil() * t.scale
}

// baselineIn returns the baseline y that vertically centres this face's
// ascent/descent box inside [top, top+height).
func (t typesetter) baselineIn(top, height int) int {
	a, d := t.Ascent(), t.Descent()
	return top + (height-(a+d))/2 + a
}

// fit truncates s with an ASCII ellipsis until it fits within maxW pixels.
func (t typesetter) fit(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if t.Measure(s) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		cand := strings.TrimRight(string(r), " ") + "..."
		if t.Measure(cand) <= maxW {
			return cand
		}
	}
	return ""
}

// wrap breaks s into lines that fit maxW, honouring explicit newlines. Words
// longer than the line are broken by rune rather than allowed to overhang.
func (t typesetter) wrap(s string, maxW int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			cand := word
			if line != "" {
				cand = line + " " + word
			}
			if t.Measure(cand) <= maxW || line == "" && t.Measure(word) <= maxW {
				line = cand
				continue
			}
			if line != "" {
				out = append(out, line)
				line = ""
			}
			// The word alone still does not fit: break it.
			for t.Measure(word) > maxW && len([]rune(word)) > 1 {
				r := []rune(word)
				n := len(r)
				for n > 1 && t.Measure(string(r[:n])) > maxW {
					n--
				}
				out = append(out, string(r[:n]))
				word = string(r[n:])
			}
			line = word
		}
		out = append(out, line)
	}
	return out
}

// ---------------------------------------------------------------------------
// Bitmap glyph masks.
//
// A bitmap face is never handed to the renderer scaled. The string is drawn
// once at 1x into an alpha mask, the mask is thresholded at alphaCutoff, and
// then each mask pixel is replicated scale x scale into the canvas. That is
// why a 2x render is exactly the 1x render with every pixel doubled, and why
// no resampling filter can ever get near a bitmap glyph.
func (t typesetter) mask(s string) *image.Gray {
	w := font.MeasureString(t.face, s).Ceil()
	m := t.face.Metrics()
	a, d := m.Ascent.Ceil(), m.Descent.Ceil()
	h := a + d
	if w <= 0 || h <= 0 {
		return image.NewGray(image.Rect(0, 0, 0, 0))
	}
	g := image.NewGray(image.Rect(0, 0, w, h))
	dr := &font.Drawer{
		Dst:  g,
		Src:  image.NewUniform(color.Gray{Y: 0xFF}),
		Face: t.face,
		Dot:  fixed.P(0, a),
	}
	dr.DrawString(s)
	return g
}
