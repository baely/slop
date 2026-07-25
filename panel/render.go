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

// Params is a fully validated render request. W and H are the COMPOSITION
// size; rotation is applied afterwards, so the delivered image is OutW x OutH.
type Params struct {
	W, H   int
	Rotate int
	Invert bool
}

// paramsFor starts from a screen's own settings; query parameters override.
func paramsFor(sc Screen) Params {
	return Params{W: sc.W, H: sc.H, Rotate: sc.Rotate, Invert: sc.Invert}
}

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

// OutW and OutH are the delivered dimensions, after rotation.
func (p Params) OutW() int {
	if p.Rotate == 90 || p.Rotate == 270 {
		return p.H
	}
	return p.W
}

func (p Params) OutH() int {
	if p.Rotate == 90 || p.Rotate == 270 {
		return p.W
	}
	return p.H
}

// Stride is the packed 1bpp row length in bytes.
func (p Params) Stride() int { return (p.OutW() + 7) / 8 }

// BinLen is the exact byte length of the .bin body for these params.
func (p Params) BinLen() int { return p.Stride() * p.OutH() }

// paramError is a client mistake: it maps to 400 with the message shown.
type paramError struct{ msg string }

func (e paramError) Error() string { return e.msg }

// ParseParams validates query parameters against a screen's own settings.
// Every failure is a clear sentence, because the thing on the other end is
// usually a person holding a soldering iron at 11pm.
func ParseParams(q url.Values, sc Screen) (Params, error) {
	p := paramsFor(sc)

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

// RenderCtx supplies everything the renderer needs from the outside world.
// Both hooks are total functions that never block: a missing image draws a
// placeholder, a missing data value draws its fallback.
type RenderCtx struct {
	Now   time.Time
	Data  func(Block) (string, bool)
	Image func(string) (*image.Gray, bool)
}

func (c RenderCtx) data(b Block) (string, bool) {
	if c.Data == nil {
		return b.Fallback, true
	}
	return c.Data(b)
}

func (c RenderCtx) image(id string) (*image.Gray, bool) {
	if c.Image == nil {
		return nil, false
	}
	return c.Image(id)
}

// Render composes a screen and returns a 1-bit paletted image whose bounds are
// exactly OutW x OutH. It is a pure function of (screen, ctx, params): the
// same content at the same instant produces the identical image, which is what
// makes the ETag worth anything.
func Render(sc Screen, p Params, ctx RenderCtx) *image.Paletted {
	ctx.Now = ctx.Now.Truncate(sc.Granularity())

	c := newCanvas(p.W, p.H)
	for _, b := range sc.Blocks {
		drawBlock(c, b.Normalize(), ctx)
	}

	g := c.g
	if p.Rotate != 0 {
		g = rotateGray(g, p.Rotate)
	}

	return toPaletted(g, p.Invert)
}

// toPaletted is the one and only path from the greyscale scratch buffer to the
// output, and the only place the threshold is applied. No grey pixel can reach
// a caller without going through here.
func toPaletted(g *image.Gray, invert bool) *image.Paletted {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	out := image.NewPaletted(image.Rect(0, 0, w, h), panelPalette)
	on, off := idxBlack, idxWhite
	if invert {
		on, off = idxWhite, idxBlack
	}
	for y := 0; y < h; y++ {
		src := g.Pix[y*g.Stride : y*g.Stride+w]
		dst := out.Pix[y*out.Stride : y*out.Stride+w]
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

// sampleCap is the largest size the editor's sample strip is drawn at. The
// strip exists to show whether a pairing survives thresholding, and above this
// every face in the list obviously does — so there is nothing to learn from a
// 300px specimen except a very tall page.
const sampleCap = 72

// SampleStrip renders a short line of text in one face at one size, through the
// real renderer. The editor shows it at 1:1 (never CSS-scaled, which would
// resample the very thing being judged) so a font/size choice is visible before
// it is committed to a screen.
func SampleStrip(fontID string, size int) *image.Paletted {
	if size > sampleCap {
		size = sampleCap
	}
	ts := typeset(fontID, size)

	// Shorter text as the size grows, so the strip stays a sensible width.
	sample := "Bin Night 09:15 3.94V ao0O1lI"
	switch {
	case ts.size > 48:
		sample = "Ag 09:15"
	case ts.size > 24:
		sample = "Bin Night 09:15"
	}

	lh := ts.LineHeight()
	w := ts.Measure(sample) + 16
	h := lh + 12
	if w > 900 {
		w = 900
	}
	if w < 32 {
		w = 32
	}
	c := newCanvas(w, h)
	c.drawString(ts, 8, ts.baselineIn(6, lh), ts.fit(sample, w-16), inkBlack)
	return toPaletted(c.g, false)
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

// ---------------------------------------------------------------- block draw

// drawBlock paints one block through a clipped sub-canvas. Nothing a block
// draws can land outside its own box, because the clip is enforced by the
// canvas view rather than by each routine remembering its bounds.
func drawBlock(root *canvas, b Block, ctx RenderCtx) {
	if b.W <= 0 || b.H <= 0 {
		return
	}
	c := root.sub(b.X, b.Y, b.W, b.H)

	ink, paper := inkBlack, inkWhite
	if b.Invert {
		c.fill(0, 0, b.W, b.H, inkBlack)
		ink, paper = inkWhite, inkBlack
	}
	_ = paper

	switch b.Type {
	case BlockText:
		drawLines(c, b, splitText(b), ink)
	case BlockClock:
		f, ok := findFormat(clockFormats, b.Format)
		if !ok {
			f = clockFormats[0]
		}
		drawLines(c, b, []string{ctx.Now.Format(f.Layout)}, ink)
	case BlockDate:
		f, ok := findFormat(dateFormats, b.Format)
		if !ok {
			f = dateFormats[0]
		}
		drawLines(c, b, []string{ctx.Now.Format(f.Layout)}, ink)
	case BlockData:
		text, stale := ctx.data(b)
		lines := []string{text}
		if stale {
			// A stale reading is marked, not hidden: a panel showing an hour-old
			// number as if it were current is worse than one that admits it.
			lines[0] = text + " ·"
		}
		drawLines(c, b, lines, ink)
	case BlockList:
		drawList(c, b, ink)
	case BlockRows:
		drawRows(c, b, ink)
	case BlockDivider:
		drawDivider(c, b, ink)
	case BlockBox:
		if b.Fill {
			c.fill(0, 0, b.W, b.H, ink)
		} else {
			c.rect(0, 0, b.W, b.H, b.Thickness, ink)
		}
	case BlockProgress:
		drawProgress(c, b, ink)
	case BlockImage:
		drawImage(c, b, ctx, ink)
	}
}

func splitText(b Block) []string {
	if b.Text == "" {
		return nil
	}
	return strings.Split(b.Text, "\n")
}

// drawLines lays a paragraph out inside the block: wrapped or truncated to the
// width, anchored as a block, aligned line by line.
func drawLines(c *canvas, b Block, raw []string, ink uint8) {
	if len(raw) == 0 {
		return
	}
	ts := typeset(b.Font, b.Size)

	var lines []string
	if b.Wrap {
		for _, l := range raw {
			lines = append(lines, ts.wrap(l, b.W)...)
		}
	} else {
		for _, l := range raw {
			lines = append(lines, ts.fit(l, b.W))
		}
	}

	lh := ts.LineHeight()
	blockH := lh * len(lines)
	widest := 0
	for _, l := range lines {
		if w := ts.Measure(l); w > widest {
			widest = w
		}
	}
	ox, oy := anchorPos(b.Anchor, b.W, b.H, widest, blockH)
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}

	for i, l := range lines {
		top := oy + i*lh
		x := ox + alignX(b.Align, widest, ts.Measure(l))
		c.drawString(ts, x, ts.baselineIn(top, lh), l, ink)
	}
}

func drawList(c *canvas, b Block, ink uint8) {
	if len(b.Items) == 0 {
		return
	}
	ts := typeset(b.Font, b.Size)
	lh := ts.LineHeight()
	gap := lh / 6
	if gap < 1 {
		gap = 1
	}
	step := lh + gap

	bullet := ""
	if b.Bullets {
		bullet = "- "
	}
	lines := make([]string, 0, len(b.Items))
	for _, it := range b.Items {
		lines = append(lines, ts.fit(bullet+it, b.W))
	}
	// Only as many as fit; the rest collapse into a +N line rather than
	// overflowing the box.
	fits := b.H / step
	if fits < 1 {
		fits = 1
	}
	overflow := 0
	if len(lines) > fits {
		overflow = len(lines) - (fits - 1)
		if fits == 1 {
			overflow = len(lines)
			lines = nil
		} else {
			lines = lines[:fits-1]
		}
	}
	if overflow > 0 {
		lines = append(lines, fmt.Sprintf("+%d more", overflow))
	}

	widest := 0
	for _, l := range lines {
		if w := ts.Measure(l); w > widest {
			widest = w
		}
	}
	ox, oy := anchorPos(b.Anchor, b.W, b.H, widest, step*len(lines)-gap)
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}
	for i, l := range lines {
		top := oy + i*step
		x := ox + alignX(b.Align, widest, ts.Measure(l))
		c.drawString(ts, x, ts.baselineIn(top, lh), l, ink)
	}
}

func drawRows(c *canvas, b Block, ink uint8) {
	if len(b.Rows) == 0 {
		return
	}
	ts := typeset(b.Font, b.Size)
	lh := ts.LineHeight()
	pad := lh / 3
	if pad < 2 {
		pad = 2
	}
	rowH := lh + pad

	rows := b.Rows
	fits := b.H / rowH
	if fits < 1 {
		fits = 1
	}
	overflow := 0
	if len(rows) > fits {
		if fits == 1 {
			overflow = len(rows)
			rows = nil
		} else {
			overflow = len(rows) - (fits - 1)
			rows = rows[:fits-1]
		}
	}

	shown := len(rows)
	if overflow > 0 {
		shown++
	}
	// A handful of rows on a tall block: let them breathe rather than stacking
	// at the top of all that white.
	if used := shown * rowH; used < b.H && shown > 0 {
		if stretch := b.H / shown; stretch > rowH {
			if lim := rowH * 3 / 2; stretch > lim {
				stretch = lim
			}
			rowH = stretch
		}
	}
	_, oy := anchorPos(b.Anchor, b.W, b.H, b.W, rowH*shown)
	if oy < 0 {
		oy = 0
	}

	y := oy
	hair := 1
	draw := func(label, value string, rule bool) {
		vw := ts.Measure(value)
		c.drawStringRight(ts, b.W, ts.baselineIn(y, rowH), value, ink)
		c.drawString(ts, 0, ts.baselineIn(y, rowH), ts.fit(label, b.W-vw-lh/2), ink)
		y += rowH
		if rule && b.Rules {
			c.fill(0, y-hair, b.W, hair, ink)
		}
	}
	for i, r := range rows {
		last := i == len(rows)-1 && overflow == 0
		draw(r.Label, ts.fit(r.Value, b.W*2/3), !last)
	}
	if overflow > 0 {
		draw(fmt.Sprintf("+%d more", overflow), "", false)
	}
}

func drawDivider(c *canvas, b Block, ink uint8) {
	t := b.Thickness
	if t < 1 {
		t = 1
	}
	if b.Orient == "vertical" {
		if t > b.W {
			t = b.W
		}
		x, _ := anchorPos(b.Anchor, b.W, b.H, t, b.H)
		c.fill(x, 0, t, b.H, ink)
		return
	}
	if t > b.H {
		t = b.H
	}
	_, y := anchorPos(b.Anchor, b.W, b.H, b.W, t)
	c.fill(0, y, b.W, t, ink)
}

func drawProgress(c *canvas, b Block, ink uint8) {
	ts := typeset(b.Font, b.Size)
	v := b.Value
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	pct := strconv.Itoa(int(v+0.5)) + "%"

	top := 0
	barH := b.H
	if b.Label != "" || b.H > ts.LineHeight()*2 {
		lh := ts.LineHeight()
		if lh < b.H {
			c.drawString(ts, 0, ts.baselineIn(0, lh), ts.fit(b.Label, b.W-ts.Measure(pct)-8), ink)
			c.drawStringRight(ts, b.W, ts.baselineIn(0, lh), pct, ink)
			top = lh + 2
			barH = b.H - top
		}
	}
	if barH < 3 {
		barH = 3
	}
	if top+barH > b.H {
		barH = b.H - top
	}
	if barH <= 0 {
		return
	}
	border := 1
	if barH >= 12 {
		border = 2
	}
	c.rect(0, top, b.W, barH, border, ink)
	inner := b.W - 2*border - 2
	if inner < 0 {
		inner = 0
	}
	filled := int(float64(inner)*v/100 + 0.5)
	c.fill(border+1, top+border+1, filled, barH-2*border-2, ink)
}

func drawImage(c *canvas, b Block, ctx RenderCtx, ink uint8) {
	src, ok := ctx.image(b.Image)
	if !ok || src.Rect.Dx() == 0 || src.Rect.Dy() == 0 {
		// Nothing to show: an outlined box with a cross, so a missing image is
		// obvious on a panel across the room rather than an empty rectangle.
		c.rect(0, 0, b.W, b.H, 1, ink)
		for i := 0; i < b.W && i < b.H; i++ {
			c.set(i*b.W/max(b.W, b.H), i*b.H/max(b.W, b.H), ink)
			c.set(b.W-1-i*b.W/max(b.W, b.H), i*b.H/max(b.W, b.H), ink)
		}
		return
	}
	dst, srcRect := fitRect(b.Fit, src.Rect.Dx(), src.Rect.Dy(), b.W, b.H)
	if dst.Dx() <= 0 || dst.Dy() <= 0 {
		return
	}
	scaled := scaleGray(src, srcRect, dst.Dx(), dst.Dy())
	dither(scaled, b.Dither)
	x, y := anchorPos(b.Anchor, b.W, b.H, dst.Dx(), dst.Dy())
	c.blitGray(x, y, scaled)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ------------------------------------------------------------------ encoding

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

// PackBits produces the .bin payload: packed 1bpp, MSB first, row-major, each
// row padded to a whole byte. Bit set (1) means a WHITE pixel, which is what
// framebuf.MONO_HLSB and the Waveshare drivers expect.
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
