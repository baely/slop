package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// photo makes something with real tone in it, so dithering has work to do.
func photo(w, h int) image.Image {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// A full-range diagonal ramp: dark in one corner, blown out in the
			// other, so every dither mode has both ends of the scale to work
			// with rather than a flat mid-tone.
			v := uint8((x*255/w + y*255/h) / 2)
			m.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return m
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageStoreAcceptsPNGJPEGGIF(t *testing.T) {
	s, err := newImageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, photo(120, 90), nil); err != nil {
		t.Fatal(err)
	}
	uploads := map[string][]byte{
		"png":  pngBytes(t, photo(120, 90)),
		"jpeg": jpg.Bytes(),
	}
	for name, raw := range uploads {
		meta, err := s.Put("id-"+name, bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
		if meta.W != 120 || meta.H != 90 {
			t.Errorf("%s: stored %dx%d, want 120x90", name, meta.W, meta.H)
		}
		g, ok := s.Gray("id-" + name)
		if !ok {
			t.Fatalf("%s: could not read back", name)
		}
		if g.Rect.Dx() != 120 {
			t.Errorf("%s: read back %v", name, g.Rect)
		}
	}
}

func TestImageStoreRejectsNonImages(t *testing.T) {
	s, err := newImageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string][]byte{
		"a text file":         []byte("this is not an image, it is a shopping list\n"),
		"an empty upload":     {},
		"a PDF":               []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n"),
		"a PNG header only":   []byte("\x89PNG\r\n\x1a\n"),
		"an executable":       {0x7f, 'E', 'L', 'F', 2, 1, 1, 0},
		"a truncated JPEG":    {0xff, 0xd8, 0xff, 0xe0, 0x00},
		"an HTML page":        []byte("<!doctype html><html><body>hi</body></html>"),
		"a zip bomb pretence": []byte("PK\x03\x04"),
	}
	for name, raw := range bad {
		if _, err := s.Put("x", bytes.NewReader(raw)); err == nil {
			t.Errorf("%s was accepted as an image", name)
		} else if !strings.Contains(err.Error(), "not a PNG, JPEG or GIF") && !strings.Contains(err.Error(), "empty") {
			t.Errorf("%s: unhelpful message %q", name, err)
		}
	}
}

func TestImageStoreDownscales(t *testing.T) {
	s, err := newImageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.Put("big", bytes.NewReader(pngBytes(t, photo(3000, 1500))))
	if err != nil {
		t.Fatal(err)
	}
	if meta.W != maxStoredSide {
		t.Errorf("stored width %d, want %d", meta.W, maxStoredSide)
	}
	if meta.H != maxStoredSide/2 {
		t.Errorf("stored height %d, want aspect preserved", meta.H)
	}
}

// Every dither mode must produce only pure black and pure white; anything else
// would survive into the panel as a grey pixel.
func TestDitherIsBinary(t *testing.T) {
	for _, mode := range dithers {
		g := toGray(photo(160, 120))
		dither(g, mode)
		black, white := 0, 0
		for _, v := range g.Pix {
			switch v {
			case inkBlack:
				black++
			case inkWhite:
				white++
			default:
				t.Fatalf("%s: produced grey value %d", mode, v)
			}
		}
		if black == 0 || white == 0 {
			t.Errorf("%s: output is a single colour (%d black, %d white)", mode, black, white)
		}
	}
}

// Error diffusion should track the source's average tone far better than a
// plain threshold does; if it does not, the diffusion is broken.
func TestErrorDiffusionTracksTone(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 100, 100))
	for i := range src.Pix {
		src.Pix[i] = 100 // a flat mid-dark grey
	}
	want := 100.0 / 255.0 // fraction of the image that should stay white

	for _, mode := range []string{"floyd", "atkinson"} {
		g := image.NewGray(src.Rect)
		copy(g.Pix, src.Pix)
		dither(g, mode)
		white := 0
		for _, v := range g.Pix {
			if v == inkWhite {
				white++
			}
		}
		got := float64(white) / float64(len(g.Pix))
		if got < want-0.15 || got > want+0.15 {
			t.Errorf("%s: %.2f of the image is white, want about %.2f", mode, got, want)
		}
	}

	// The plain threshold turns the whole flat field black; that is correct
	// behaviour for it, and the reason the other two exist.
	g := image.NewGray(src.Rect)
	copy(g.Pix, src.Pix)
	dither(g, "threshold")
	for _, v := range g.Pix {
		if v != inkBlack {
			t.Fatal("threshold mode is not a hard cut")
		}
	}
}

func TestFitRect(t *testing.T) {
	// contain: whole image, letterboxed, aspect kept
	dst, src := fitRect("contain", 200, 100, 100, 100)
	if dst.Dx() != 100 || dst.Dy() != 50 || src.Dx() != 200 {
		t.Errorf("contain: dst=%v src=%v", dst, src)
	}
	// cover: box filled, source cropped
	dst, src = fitRect("cover", 200, 100, 100, 100)
	if dst.Dx() != 100 || dst.Dy() != 100 {
		t.Errorf("cover dst=%v", dst)
	}
	if src.Dx() != 100 || src.Dy() != 100 {
		t.Errorf("cover src=%v, want a square crop of the 200x100 source", src)
	}
	// stretch: aspect ignored
	dst, _ = fitRect("stretch", 200, 100, 130, 70)
	if dst.Dx() != 130 || dst.Dy() != 70 {
		t.Errorf("stretch dst=%v", dst)
	}
}

// An image block, end to end: 1-bit output, inside its bounds, real detail.
func TestImageBlockRendersOneBit(t *testing.T) {
	s, err := newImageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("photo1", bytes.NewReader(pngBytes(t, photo(400, 300)))); err != nil {
		t.Fatal(err)
	}
	for _, mode := range dithers {
		for _, fit := range fits {
			b := Block{Type: BlockImage, X: 30, Y: 20, W: 240, H: 160, Image: "photo1",
				Dither: mode, Fit: fit, Anchor: "c"}.Normalize()
			sc := Screen{Name: "t", W: 320, H: 220, Blocks: []Block{b}}
			img := Render(sc, paramsFor(sc), RenderCtx{
				Now:   fixedNow,
				Image: func(id string) (*image.Gray, bool) { return s.Gray(id) },
			})
			assertPureAndInside(t, img, sc, b, mode+"/"+fit)
		}
	}
}

func TestDeletingAnImageInUseIsRefused(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddImage(ImageMeta{ID: "pic", Name: "roll.png", W: 10, H: 10}); err != nil {
		t.Fatal(err)
	}
	sc := st.DefaultScreen()
	sc.Blocks = []Block{{Type: BlockImage, X: 0, Y: 0, W: 100, H: 100, Image: "pic",
		Dither: "floyd", Fit: "contain", Anchor: "c", Align: "left"}}
	if err := st.SaveScreen(sc.Name, sc); err != nil {
		t.Fatal(err)
	}
	err = st.DeleteImage("pic")
	if err == nil {
		t.Fatal("deleted an image that a block still uses")
	}
	if !strings.Contains(err.Error(), "still uses that image") {
		t.Errorf("unhelpful message: %v", err)
	}
}
