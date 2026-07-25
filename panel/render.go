package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Dimension caps. A battery panel asking for something absurd is a bug, not a
// feature; rendering is bounded so a stranger cannot turn this into a CPU sink.
const (
	minDim  = 64
	maxDim  = 2400
	maxArea = 3_000_000 // 1404x1872 (Supernote) = 2,628,288, comfortably inside
)

// Preset panels. Keys are accepted by the `preset` query parameter.
type preset struct {
	Name string
	W, H int
	Note string
}

var presets = []preset{
	{"800x480", 800, 480, "Waveshare 7.5in"},
	{"400x300", 400, 300, "Waveshare 4.2in"},
	{"1404x1872", 1404, 1872, "Supernote A5X"},
}

// Params is a fully validated render request.
type Params struct {
	W, H   int
	Rotate int
	Invert bool
}

func defaultParams() Params { return Params{W: 800, H: 480} }

// Query renders the params back to a query string (stable key order).
func (p Params) Query() string {
	v := url.Values{}
	v.Set("w", strconv.Itoa(p.W))
	v.Set("h", strconv.Itoa(p.H))
	if p.Rotate != 0 {
		v.Set("rotate", strconv.Itoa(p.Rotate))
	}
	if p.Invert {
		v.Set("invert", "1")
	}
	return v.Encode()
}

// Stride is the packed 1bpp row length in bytes.
func (p Params) Stride() int { return (p.W + 7) / 8 }

// BinLen is the exact byte length of /screen.bin for these params.
func (p Params) BinLen() int { return p.Stride() * p.H }

// paramError is a client mistake: it maps to 400 with the message shown.
type paramError struct{ msg string }

func (e paramError) Error() string { return e.msg }

// ParseParams validates query parameters. Every failure is a clear sentence,
// because the thing on the other end is usually a person holding a soldering
// iron at 11pm.
func ParseParams(q url.Values) (Params, error) {
	p := defaultParams()

	if name := strings.TrimSpace(q.Get("preset")); name != "" {
		ok := false
		for _, pr := range presets {
			if strings.EqualFold(pr.Name, name) {
				p.W, p.H, ok = pr.W, pr.H, true
				break
			}
		}
		if !ok {
			names := make([]string, len(presets))
			for i, pr := range presets {
				names[i] = pr.Name
			}
			return p, paramError{fmt.Sprintf("unknown preset %q. known presets: %s", name, strings.Join(names, ", "))}
		}
	}

	for _, f := range []struct {
		key string
		dst *int
	}{{"w", &p.W}, {"h", &p.H}} {
		raw := strings.TrimSpace(q.Get(f.key))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return p, paramError{fmt.Sprintf("%s must be a whole number of pixels, got %q", f.key, raw)}
		}
		if n < minDim || n > maxDim {
			return p, paramError{fmt.Sprintf("%s must be between %d and %d pixels, got %d", f.key, minDim, maxDim, n)}
		}
		*f.dst = n
	}

	if p.W*p.H > maxArea {
		return p, paramError{fmt.Sprintf("%dx%d is %d pixels; the cap is %d", p.W, p.H, p.W*p.H, maxArea)}
	}

	if raw := strings.TrimSpace(q.Get("rotate")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || (n != 0 && n != 90 && n != 180 && n != 270) {
			return p, paramError{fmt.Sprintf("rotate must be 0, 90, 180 or 270, got %q", raw)}
		}
		p.Rotate = n
	}

	if raw := strings.TrimSpace(q.Get("invert")); raw != "" {
		switch strings.ToLower(raw) {
		case "1", "true", "yes", "on":
			p.Invert = true
		case "0", "false", "no", "off":
			p.Invert = false
		default:
			return p, paramError{fmt.Sprintf("invert must be 0 or 1, got %q", raw)}
		}
	}

	return p, nil
}

// Palette index 0 is black, index 1 is white, always — invert flips the pixels,
// not the palette, so the packed bit meaning never moves.
const (
	idxBlack uint8 = 0
	idxWhite uint8 = 1
)

var panelPalette = color.Palette{
	color.Gray{Y: 0x00},
	color.Gray{Y: 0xFF},
}

// threshold: greyscale values at or above this become white. 128 is the honest
// midpoint; nudged up slightly so thresholded stems keep their weight.
const threshold = 140

// Render composes the dashboard and returns a 1-bit paletted image whose
// bounds are exactly p.W x p.H. It is a pure function of (cfg, now, p): the
// same content in the same minute produces the identical image, which is what
// makes the ETag worth anything.
func Render(cfg Config, now time.Time, p Params) *image.Paletted {
	now = now.Truncate(time.Minute)

	// The content is composed at the pre-rotation size, then rotated, so the
	// response is always exactly p.W x p.H no matter the rotation.
	cw, ch := p.W, p.H
	if p.Rotate == 90 || p.Rotate == 270 {
		cw, ch = p.H, p.W
	}

	c := newCanvas(cw, ch)
	layout(c, cfg, now)

	g := c.g
	if p.Rotate != 0 {
		g = rotateGray(g, p.Rotate)
	}

	out := image.NewPaletted(image.Rect(0, 0, p.W, p.H), panelPalette)
	on, off := idxBlack, idxWhite
	if p.Invert {
		on, off = idxWhite, idxBlack
	}
	for y := 0; y < p.H; y++ {
		src := g.Pix[y*g.Stride : y*g.Stride+p.W]
		dst := out.Pix[y*out.Stride : y*out.Stride+p.W]
		for x, v := range src {
			if v < threshold {
				dst[x] = on
			} else {
				dst[x] = off
			}
		}
	}
	return out
}

func rotateGray(src *image.Gray, deg int) *image.Gray {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	var dst *image.Gray
	switch deg {
	case 90:
		dst = image.NewGray(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Pix[x*dst.Stride+(h-1-y)] = src.Pix[y*src.Stride+x]
			}
		}
	case 180:
		dst = image.NewGray(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Pix[(h-1-y)*dst.Stride+(w-1-x)] = src.Pix[y*src.Stride+x]
			}
		}
	case 270:
		dst = image.NewGray(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Pix[(w-1-x)*dst.Stride+y] = src.Pix[y*src.Stride+x]
			}
		}
	default:
		return src
	}
	return dst
}

// layout paints the dashboard onto the greyscale canvas.
//
//	+--------------------------------------------------+
//	| TITLE                                    (band)  |  header, black fill
//	|                                                  |
//	|  14:32                             Saturday      |  clock block
//	|                                 25 July 2026     |
//	|==================================================|
//	|  Label                                   Value   |  rows
//	|  Label                                   Value   |
//	|--------------------------------------------------|
//	|  footer note                    Updated 14:32    |
//	+--------------------------------------------------+
func layout(c *canvas, cfg Config, now time.Time) {
	// Everything scales off an 800x480 reference so a 400x300 badge and a
	// 1404x1872 Supernote both come out proportioned rather than one of them
	// having a clock the size of a dinner plate.
	sx := float64(c.w) / 800.0
	sy := float64(c.h) / 480.0
	s := sx
	if sy < s {
		s = sy
	}
	px := func(ref float64, min int) int {
		v := int(ref*s + 0.5)
		if v < min {
			v = min
		}
		return v
	}

	pad := px(20, 4)
	bandH := px(52, 14)
	clockH := px(132, 30)
	ruleH := px(3, 1)
	footH := px(34, 12)
	rowH := px(42, 14)

	// ---- header band ----
	c.fill(0, 0, c.w, bandH, inkBlack)
	titleFace := face(fBold, px(29, 8))
	title := fit(titleFace, cfg.Title, c.w-2*pad)
	c.text(titleFace, pad, baselineIn(titleFace, 0, bandH), title, inkWhite)

	// ---- clock block ----
	top := bandH
	clockFace := face(fBold, px(112, 16))
	clock := now.Format("15:04")
	base := baselineIn(clockFace, top, clockH)
	c.text(clockFace, pad, base, clock, inkBlack)

	dayFace := face(fBold, px(30, 9))
	dateFace := face(fRegular, px(26, 8))
	dayA, dayD := vmetrics(dayFace)
	dateA, dateD := vmetrics(dateFace)
	gap := px(6, 1)
	blockH := dayA + dayD + gap + dateA + dateD
	dayTop := top + (clockH-blockH)/2
	rightX := c.w - pad
	left := pad + measure(clockFace, clock) + px(16, 4)
	avail := rightX - left
	c.textRight(dayFace, rightX, dayTop+dayA, fit(dayFace, now.Format("Monday"), avail), inkBlack)
	c.textRight(dateFace, rightX, dayTop+dayA+dayD+gap+dateA, fit(dateFace, now.Format("2 January 2006"), avail), inkBlack)

	// ---- separator ----
	top += clockH
	c.fill(0, top, c.w, ruleH, inkBlack)
	top += ruleH

	// ---- footer (measured from the bottom) ----
	footTop := c.h - footH
	hair := px(1, 1)
	c.fill(pad, footTop, c.w-2*pad, hair, inkBlack)
	footFace := face(fRegular, px(15, 8))
	stampFace := face(fMono, px(15, 8))
	stamp := "Updated " + now.Format("15:04 Mon 2 Jan")
	c.textRight(stampFace, c.w-pad, baselineIn(stampFace, footTop+hair, footH-hair), stamp, inkBlack)
	if cfg.Footer != "" {
		maxW := c.w - 2*pad - measure(stampFace, stamp) - px(16, 4)
		c.text(footFace, pad, baselineIn(footFace, footTop+hair, footH-hair), fit(footFace, cfg.Footer, maxW), inkBlack)
	}

	// ---- rows ----
	region := footTop - top
	if region < rowH {
		return
	}
	maxRows := region / rowH
	rows := cfg.Rows

	if len(rows) == 0 {
		f := face(fRegular, px(20, 8))
		c.textCenter(f, c.w/2, baselineIn(f, top, region), "0 rows.", inkBlack)
		return
	}

	overflow := 0
	if len(rows) > maxRows {
		overflow = len(rows) - (maxRows - 1)
		rows = rows[:maxRows-1]
	}

	// Few rows on a tall panel: let them breathe a little, then centre the
	// block in the region. A Supernote is 1872px tall and a hard-left-top
	// stack of six rows in all that white looks like a bug, not a layout.
	shown := len(rows)
	if overflow > 0 {
		shown++
	}
	if used := shown * rowH; used < region {
		if stretch := region / shown; stretch > rowH {
			if lim := rowH * 3 / 2; stretch > lim {
				stretch = lim
			}
			rowH = stretch
		}
		top += (region - shown*rowH) / 2
	}

	labelFace := face(fRegular, px(18, 8))
	valueFace := face(fMonoBold, px(19, 8))
	y := top
	draw := func(label, value string, rule bool) {
		vw := measure(valueFace, value)
		c.textRight(valueFace, c.w-pad, baselineIn(valueFace, y, rowH), value, inkBlack)
		c.text(labelFace, pad, baselineIn(labelFace, y, rowH), fit(labelFace, label, c.w-2*pad-vw-px(12, 3)), inkBlack)
		y += rowH
		if rule {
			c.fill(pad, y-hair, c.w-2*pad, hair, inkBlack)
		}
	}
	for i, r := range rows {
		last := i == len(rows)-1 && overflow == 0
		draw(r.Label, fit(valueFace, r.Value, (c.w-2*pad)*2/3), !last)
	}
	if overflow > 0 {
		draw(fmt.Sprintf("+%d more", overflow), "", false)
	}
}

// EncodePNG writes the image as a true 1-bit paletted PNG: bit depth 1, colour
// type 3, a two-entry PLTE of black and white. Go's encoder picks bit depth 1
// automatically for a *image.Paletted with a palette of two.
func EncodePNG(img *image.Paletted) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PackBits produces the /screen.bin payload: packed 1bpp, MSB first, row-major,
// each row padded to a whole byte. Bit set (1) means a WHITE pixel, which is
// what framebuf.MONO_HLSB and the Waveshare drivers expect.
//
//	byte index = y*ceil(w/8) + x/8
//	bit        = 0x80 >> (x % 8)
func PackBits(img *image.Paletted) []byte {
	w := img.Rect.Dx()
	h := img.Rect.Dy()
	stride := (w + 7) / 8
	out := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		row := out[y*stride : (y+1)*stride]
		src := img.Pix[y*img.Stride : y*img.Stride+w]
		for x, v := range src {
			if v == idxWhite {
				row[x>>3] |= 0x80 >> uint(x&7)
			}
		}
	}
	return out
}
