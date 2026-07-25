package main

import (
	"image"

	xdraw "golang.org/x/image/draw"
)

// fitRect works out where a source of size (sw,sh) lands inside a box of size
// (bw,bh) under the given fit mode, and which part of the source is used.
//
//	contain — whole image, letterboxed
//	cover   — box filled, source cropped
//	stretch — aspect ratio ignored
func fitRect(mode string, sw, sh, bw, bh int) (dst image.Rectangle, src image.Rectangle) {
	src = image.Rect(0, 0, sw, sh)
	if sw <= 0 || sh <= 0 || bw <= 0 || bh <= 0 {
		return image.Rect(0, 0, 0, 0), src
	}
	switch mode {
	case "stretch":
		return image.Rect(0, 0, bw, bh), src

	case "cover":
		scale := float64(bw) / float64(sw)
		if s := float64(bh) / float64(sh); s > scale {
			scale = s
		}
		// Crop the source to the box's aspect ratio, centred.
		cw := int(float64(bw)/scale + 0.5)
		ch := int(float64(bh)/scale + 0.5)
		if cw > sw {
			cw = sw
		}
		if ch > sh {
			ch = sh
		}
		src = image.Rect((sw-cw)/2, (sh-ch)/2, (sw-cw)/2+cw, (sh-ch)/2+ch)
		return image.Rect(0, 0, bw, bh), src

	default: // contain
		scale := float64(bw) / float64(sw)
		if s := float64(bh) / float64(sh); s < scale {
			scale = s
		}
		nw := int(float64(sw)*scale + 0.5)
		nh := int(float64(sh)*scale + 0.5)
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		return image.Rect(0, 0, nw, nh), src
	}
}

// scaleGray resamples a greyscale image into a new one of the requested size.
// CatmullRom on the way down keeps the detail that error diffusion then has
// something to work with; a box filter here makes a scan look muddy.
func scaleGray(src *image.Gray, srcRect image.Rectangle, w, h int) *image.Gray {
	out := image.NewGray(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return out
	}
	xdraw.CatmullRom.Scale(out, out.Rect, src, srcRect, xdraw.Src, nil)
	return out
}

// dither converts a greyscale image in place to pure black and white.
//
// All three modes write only 0x00 and 0xFF, so the final threshold pass is a
// no-op over an image block and the dithered structure survives verbatim.
func dither(g *image.Gray, mode string) {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	if w == 0 || h == 0 {
		return
	}

	if mode == "threshold" {
		for i, v := range g.Pix {
			if v < threshold {
				g.Pix[i] = inkBlack
			} else {
				g.Pix[i] = inkWhite
			}
		}
		return
	}

	// Error diffusion needs headroom below 0 and above 255, so it runs over a
	// float buffer rather than the uint8 pixels.
	buf := make([]float32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			buf[y*w+x] = float32(g.Pix[y*g.Stride+x])
		}
	}

	add := func(x, y int, e float32) {
		if x < 0 || x >= w || y < 0 || y >= h {
			return
		}
		buf[y*w+x] += e
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := buf[y*w+x]
			var newV float32
			if old < 128 {
				newV = 0
			} else {
				newV = 255
			}
			buf[y*w+x] = newV
			err := old - newV

			switch mode {
			case "atkinson":
				// Atkinson passes on only 6/8 of the error, which is why it
				// keeps contrast and open highlights — the right look for a
				// scan on e-ink.
				e := err / 8
				add(x+1, y, e)
				add(x+2, y, e)
				add(x-1, y+1, e)
				add(x, y+1, e)
				add(x+1, y+1, e)
				add(x, y+2, e)
			default: // floyd
				add(x+1, y, err*7/16)
				add(x-1, y+1, err*3/16)
				add(x, y+1, err*5/16)
				add(x+1, y+1, err*1/16)
			}
		}
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if buf[y*w+x] < 128 {
				g.Pix[y*g.Stride+x] = inkBlack
			} else {
				g.Pix[y*g.Stride+x] = inkWhite
			}
		}
	}
}
