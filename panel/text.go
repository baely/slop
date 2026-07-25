package main

import (
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
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Font kinds available to the renderer.
type fontKind int

const (
	fRegular fontKind = iota
	fBold
	fMono
	fMonoBold
)

// bitmapFloor is the pixel size below which the outline fonts stop being worth
// thresholding. Below it we fall back to basicfont.Face7x13, a genuine 1-bit
// bitmap face (its glyph masks only ever contain 0x00 and 0xff), which stays
// razor sharp on a small panel where a thresholded outline turns to mush.
const bitmapFloor = 12

var (
	parsedOnce sync.Once
	parsed     map[fontKind]*opentype.Font
	parseErr   error

	faceMu    sync.Mutex
	faceCache = map[faceKey]font.Face{}
)

type faceKey struct {
	kind fontKind
	size int
}

func parseFonts() error {
	parsedOnce.Do(func() {
		parsed = map[fontKind]*opentype.Font{}
		for kind, raw := range map[fontKind][]byte{
			fRegular:  goregular.TTF,
			fBold:     gobold.TTF,
			fMono:     gomono.TTF,
			fMonoBold: gomonobold.TTF,
		} {
			f, err := opentype.Parse(raw)
			if err != nil {
				parseErr = err
				return
			}
			parsed[kind] = f
		}
	})
	return parseErr
}

// face returns a cached font face. Sizes are in pixels (DPI is pinned to 72 so
// point size == pixel size). Full hinting snaps stems to the pixel grid, which
// matters a lot once the render is thresholded to 1 bit.
func face(kind fontKind, size int) font.Face {
	if size < bitmapFloor {
		return basicfont.Face7x13
	}
	if size > 400 {
		size = 400
	}
	key := faceKey{kind, size}
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f
	}
	if err := parseFonts(); err != nil {
		faceCache[key] = basicfont.Face7x13
		return basicfont.Face7x13
	}
	f, err := opentype.NewFace(parsed[kind], &opentype.FaceOptions{
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

func measure(f font.Face, s string) int {
	return font.MeasureString(f, s).Ceil()
}

// ascent and descent in whole pixels.
func vmetrics(f font.Face) (int, int) {
	m := f.Metrics()
	return m.Ascent.Ceil(), m.Descent.Ceil()
}

// baselineIn returns the baseline y that vertically centres the face's
// ascent/descent box inside [top, top+height).
func baselineIn(f font.Face, top, height int) int {
	a, d := vmetrics(f)
	return top + (height-(a+d))/2 + a
}

// fit truncates s with an ASCII ellipsis until it fits within maxW pixels.
func fit(f font.Face, s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if measure(f, s) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		cand := strings.TrimRight(string(r), " ") + "..."
		if measure(f, cand) <= maxW {
			return cand
		}
	}
	return ""
}

// canvas is an 8-bit greyscale scratch buffer. Everything is composed here
// (text antialiases into it freely) and the whole thing is thresholded exactly
// once, at the end, so the output cannot contain a grey pixel.
type canvas struct {
	g    *image.Gray
	w, h int
}

const (
	inkBlack uint8 = 0x00
	inkWhite uint8 = 0xFF
)

func newCanvas(w, h int) *canvas {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		g.Pix[i] = inkWhite
	}
	return &canvas{g: g, w: w, h: h}
}

func (c *canvas) fill(x, y, w, h int, v uint8) {
	r := image.Rect(x, y, x+w, y+h).Intersect(c.g.Rect)
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		row := c.g.Pix[yy*c.g.Stride+r.Min.X : yy*c.g.Stride+r.Max.X]
		for i := range row {
			row[i] = v
		}
	}
}

// text draws s with its left edge at x and its baseline at y.
func (c *canvas) text(f font.Face, x, y int, s string, v uint8) {
	if s == "" {
		return
	}
	d := &font.Drawer{
		Dst:  c.g,
		Src:  image.NewUniform(color.Gray{Y: v}),
		Face: f,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// textRight draws s with its right edge at x.
func (c *canvas) textRight(f font.Face, x, y int, s string, v uint8) {
	c.text(f, x-measure(f, s), y, s, v)
}

// textCenter draws s centred horizontally on x.
func (c *canvas) textCenter(f font.Face, x, y int, s string, v uint8) {
	c.text(f, x-measure(f, s)/2, y, s, v)
}
