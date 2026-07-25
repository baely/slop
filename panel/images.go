package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"

	xdraw "golang.org/x/image/draw"
)

const (
	maxImages       = 20
	maxImageBytes   = 4 << 20
	maxStoredSide   = 1600
	maxDecodePixels = 40_000_000 // refuse decompression bombs before allocating
)

// imageStore keeps uploaded pictures as 8-bit greyscale PNGs under
// DATA_DIR/images. Greyscale rather than pre-dithered, so the dither mode
// stays a live per-block choice and can be changed without re-uploading.
type imageStore struct {
	dir string

	mu     sync.Mutex
	cache  map[string]*image.Gray
	cached []string
}

func newImageStore(dataDir string) (*imageStore, error) {
	dir := filepath.Join(dataDir, "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &imageStore{dir: dir, cache: map[string]*image.Gray{}}, nil
}

func (s *imageStore) path(id string) string { return filepath.Join(s.dir, id+".png") }

// Put decodes, greyscales, downscales and stores an upload. A file that is not
// a PNG, JPEG or GIF is rejected with a sentence saying so.
func (s *imageStore) Put(id string, r io.Reader) (ImageMeta, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxImageBytes+1))
	if err != nil {
		return ImageMeta{}, err
	}
	if len(raw) == 0 {
		return ImageMeta{}, errors.New("that upload was empty")
	}
	if len(raw) > maxImageBytes {
		return ImageMeta{}, fmt.Errorf("that file is over the %d MiB upload cap", maxImageBytes>>20)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return ImageMeta{}, errors.New("that file is not a PNG, JPEG or GIF")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxDecodePixels {
		return ImageMeta{}, fmt.Errorf("that %s is %dx%d, which is more pixels than this will decode", format, cfg.Width, cfg.Height)
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return ImageMeta{}, errors.New("that file is not a PNG, JPEG or GIF")
	}

	g := toGray(src)
	g = downscale(g, maxStoredSide)

	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, g); err != nil {
		return ImageMeta{}, err
	}
	if err := atomicWrite(s.path(id), buf.Bytes(), 0o600); err != nil {
		return ImageMeta{}, err
	}

	s.mu.Lock()
	s.cache[id] = g
	s.cached = append(s.cached, id)
	s.evictLocked()
	s.mu.Unlock()

	return ImageMeta{ID: id, W: g.Rect.Dx(), H: g.Rect.Dy(), Bytes: buf.Len()}, nil
}

func (s *imageStore) Remove(id string) error {
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Gray returns the stored greyscale image, loading and caching it on demand.
func (s *imageStore) Gray(id string) (*image.Gray, bool) {
	if id == "" {
		return nil, false
	}
	s.mu.Lock()
	if g, ok := s.cache[id]; ok {
		s.mu.Unlock()
		return g, true
	}
	s.mu.Unlock()

	f, err := os.Open(s.path(id))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil, false
	}
	g := toGray(src)

	s.mu.Lock()
	s.cache[id] = g
	s.cached = append(s.cached, id)
	s.evictLocked()
	s.mu.Unlock()
	return g, true
}

// PNG returns the stored bytes, for the editor's thumbnails.
func (s *imageStore) PNG(id string) ([]byte, error) {
	return os.ReadFile(s.path(id))
}

func (s *imageStore) evictLocked() {
	for len(s.cached) > maxImages {
		oldest := s.cached[0]
		s.cached = s.cached[1:]
		delete(s.cache, oldest)
	}
}

func toGray(src image.Image) *image.Gray {
	if g, ok := src.(*image.Gray); ok {
		return g
	}
	b := src.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(g, g.Rect, src, b.Min, draw.Src)
	return g
}

func downscale(g *image.Gray, maxSide int) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	if w <= maxSide && h <= maxSide {
		return g
	}
	scale := float64(maxSide) / float64(w)
	if s := float64(maxSide) / float64(h); s < scale {
		scale = s
	}
	nw, nh := int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	out := image.NewGray(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(out, out.Rect, g, g.Rect, xdraw.Src, nil)
	return out
}

// registered so image.Decode understands all three formats.
var _ = jpeg.Decode
var _ = gif.Decode
var _ = color.Gray{}
