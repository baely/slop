package main

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	inkBlack uint8 = 0x00
	inkWhite uint8 = 0xFF
)

// canvas is an 8-bit greyscale scratch buffer with an origin and a clip.
//
// Everything is composed here — outline text antialiases into it freely — and
// the whole thing is thresholded exactly once, at the very end, so no grey
// pixel can reach the output.
//
// sub() returns a view sharing the same pixels with its own origin and clip.
// Every block draws through one of those views, which is the entire mechanism
// by which a block's content cannot escape its own box: the clip is enforced
// by image.Gray.SubImage bounds, not by each drawing routine remembering to
// check.
type canvas struct {
	g      *image.Gray
	clip   image.Rectangle // in g coordinates
	ox, oy int             // where local (0,0) sits in g coordinates
	w, h   int             // local size
}

func newCanvas(w, h int) *canvas {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		g.Pix[i] = inkWhite
	}
	return &canvas{g: g, clip: g.Rect, w: w, h: h}
}

// sub returns a clipped view of the region (x,y,w,h) in local coordinates.
func (c *canvas) sub(x, y, w, h int) *canvas {
	r := image.Rect(c.ox+x, c.oy+y, c.ox+x+w, c.oy+y+h).Intersect(c.clip)
	return &canvas{g: c.g, clip: r, ox: c.ox + x, oy: c.oy + y, w: w, h: h}
}

// dst is the drawable image restricted to the clip. Anything outside it is
// silently dropped by the draw package.
func (c *canvas) dst() *image.Gray {
	if c.clip == c.g.Rect {
		return c.g
	}
	sub := c.g.SubImage(c.clip)
	if g, ok := sub.(*image.Gray); ok {
		return g
	}
	return image.NewGray(image.Rect(0, 0, 0, 0))
}

func (c *canvas) fill(x, y, w, h int, v uint8) {
	r := image.Rect(c.ox+x, c.oy+y, c.ox+x+w, c.oy+y+h).Intersect(c.clip)
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		row := c.g.Pix[yy*c.g.Stride+r.Min.X : yy*c.g.Stride+r.Max.X]
		for i := range row {
			row[i] = v
		}
	}
}

// set writes one local pixel, clipped.
func (c *canvas) set(x, y int, v uint8) {
	gx, gy := c.ox+x, c.oy+y
	if gx < c.clip.Min.X || gx >= c.clip.Max.X || gy < c.clip.Min.Y || gy >= c.clip.Max.Y {
		return
	}
	c.g.Pix[gy*c.g.Stride+gx] = v
}

// rect draws an outlined rectangle of the given thickness, inset inward.
func (c *canvas) rect(x, y, w, h, t int, v uint8) {
	if t <= 0 {
		return
	}
	if t*2 > h {
		t = (h + 1) / 2
	}
	if t*2 > w {
		t = (w + 1) / 2
	}
	c.fill(x, y, w, t, v)
	c.fill(x, y+h-t, w, t, v)
	c.fill(x, y+t, t, h-2*t, v)
	c.fill(x+w-t, y+t, t, h-2*t, v)
}

// drawString draws s with its left edge at x and its baseline at y.
//
// Outline faces go straight through font.Drawer and antialias into the
// greyscale buffer. Bitmap faces take the mask path: rendered once at 1x,
// thresholded, then replicated in whole pixels.
func (c *canvas) drawString(ts typesetter, x, y int, s string, v uint8) {
	if s == "" {
		return
	}
	if !ts.tf.Bitmap {
		d := &font.Drawer{
			Dst:  c.dst(),
			Src:  image.NewUniform(color.Gray{Y: v}),
			Face: ts.face,
			Dot:  fixed.P(c.ox+x, c.oy+y),
		}
		d.DrawString(s)
		return
	}
	m := ts.mask(s)
	mw, mh := m.Rect.Dx(), m.Rect.Dy()
	if mw == 0 || mh == 0 {
		return
	}
	top := y - ts.Ascent()
	sc := ts.scale
	for my := 0; my < mh; my++ {
		row := m.Pix[my*m.Stride : my*m.Stride+mw]
		for mx, a := range row {
			if a < alphaCutoff {
				continue
			}
			c.fill(x+mx*sc, top+my*sc, sc, sc, v)
		}
	}
}

func (c *canvas) drawStringRight(ts typesetter, x, y int, s string, v uint8) {
	c.drawString(ts, x-ts.Measure(s), y, s, v)
}

func (c *canvas) drawStringCenter(ts typesetter, x, y int, s string, v uint8) {
	c.drawString(ts, x-ts.Measure(s)/2, y, s, v)
}

// blitGray copies a greyscale image into the canvas at (x,y), clipped.
func (c *canvas) blitGray(x, y int, src *image.Gray) {
	b := src.Rect
	for sy := b.Min.Y; sy < b.Max.Y; sy++ {
		for sx := b.Min.X; sx < b.Max.X; sx++ {
			c.set(x+sx-b.Min.X, y+sy-b.Min.Y, src.Pix[sy*src.Stride+sx])
		}
	}
}

// anchorPos places a contentW x contentH box inside a boxW x boxH region.
// Anchors are compass points: nw n ne w c e sw s se.
func anchorPos(anchor string, boxW, boxH, contentW, contentH int) (int, int) {
	x, y := 0, 0
	switch anchor {
	case "n", "c", "s":
		x = (boxW - contentW) / 2
	case "ne", "e", "se":
		x = boxW - contentW
	}
	switch anchor {
	case "w", "c", "e":
		y = (boxH - contentH) / 2
	case "sw", "s", "se":
		y = boxH - contentH
	}
	return x, y
}

// alignX places a line of width lineW inside a boxW region.
func alignX(align string, boxW, lineW int) int {
	switch align {
	case "center":
		return (boxW - lineW) / 2
	case "right":
		return boxW - lineW
	default:
		return 0
	}
}
