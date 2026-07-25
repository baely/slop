package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rendered is one cached artefact plus its strong ETag.
type rendered struct {
	body []byte
	etag string
}

// renderCache memoises renders so a panel polling every minute costs one render
// per minute per shape, not one per request.
type renderCache struct {
	mu sync.Mutex
	m  map[string]*rendered
}

func newRenderCache() *renderCache { return &renderCache{m: map[string]*rendered{}} }

func (c *renderCache) get(key string) (*rendered, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.m[key]
	return r, ok
}

func (c *renderCache) put(key string, r *rendered) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) > 64 {
		c.m = map[string]*rendered{}
	}
	c.m[key] = r
}

// Server wires everything together.
type Server struct {
	auth    *Auth
	store   *Store
	images  *imageStore
	data    *dataStore
	cache   *renderCache
	loc     *time.Location
	baseURL string
	now     func() time.Time
	ctx     context.Context
}

func (s *Server) clock() time.Time { return s.now().In(s.loc) }

func (s *Server) renderCtx(now time.Time) RenderCtx {
	return RenderCtx{
		Now:   now,
		Data:  func(b Block) (string, bool) { return s.data.Lookup(b, s.now()) },
		Image: func(id string) (*image.Gray, bool) { return s.images.Gray(id) },
	}
}

// render produces (and caches) the artefacts for one request shape.
func (s *Server) render(sc Screen, p Params) (*rendered, *rendered, error) {
	now := s.clock().Truncate(sc.Granularity())
	ctx := s.renderCtx(now)

	// The cache key has to move when a data value moves, or a panel would keep
	// getting 304 for a reading that changed.
	stamp := ""
	for _, b := range sc.Blocks {
		if b.Type == BlockData {
			v, stale := ctx.data(b.Normalize())
			stamp += v
			if stale {
				stamp += "!"
			}
			stamp += "\x00"
		}
	}
	key := fmt.Sprintf("%s|%s|%s|%s|%s", sc.Name, p.Query(), now.Format(time.RFC3339), sc.Version(), shortHash(stamp))

	if pngR, ok := s.cache.get("png|" + key); ok {
		if binR, ok := s.cache.get("bin|" + key); ok {
			return pngR, binR, nil
		}
	}
	img := Render(sc, p, ctx)
	body, err := EncodePNG(img)
	if err != nil {
		return nil, nil, err
	}
	pngR := &rendered{body: body, etag: etagOf(body)}
	bin := PackBits(img)
	binR := &rendered{body: bin, etag: etagOf(bin)}
	s.cache.put("png|"+key, pngR)
	s.cache.put("bin|"+key, binR)
	return pngR, binR, nil
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// ---------------------------------------------------------------- device auth

// deviceAuthed accepts the screen's unguessable device key (query `k` or
// bearer), the app token as a bearer, or a signed session cookie so the
// preview <img> loads.
func (s *Server) deviceAuthed(r *http.Request, sc Screen) bool {
	if s.auth.Authed(r) {
		return true
	}
	cand := strings.TrimSpace(r.URL.Query().Get("k"))
	if cand == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			cand = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if cand == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cand), []byte(sc.DeviceKey)) == 1
}

// ------------------------------------------------------------------- /screen.*

// handleScreen serves a screen as PNG or packed bytes. named=false is the
// legacy path (/screen.png), which always means the first screen so anything
// already flashed onto a device keeps working.
func (s *Server) handleScreen(binary, named bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var sc Screen
		if named {
			name := r.PathValue("screen")
			var ok bool
			sc, ok = s.store.Screen(name)
			if !ok {
				http.Error(w, "no screen named "+strconv.Quote(name), http.StatusNotFound)
				return
			}
		} else {
			sc = s.store.DefaultScreen()
		}

		ip := clientIP(r)
		if s.auth.limiter.Blocked(ip, time.Now()) {
			http.Error(w, "too many failed attempts, try again in a minute", http.StatusTooManyRequests)
			return
		}
		if !s.deviceAuthed(r, sc) {
			s.auth.limiter.Fail(ip, time.Now())
			w.Header().Set("WWW-Authenticate", `Bearer realm="panel"`)
			http.Error(w, "this screen needs its device key: add ?k=<key> or an Authorization: Bearer header. The key is on the edit page.", http.StatusUnauthorized)
			return
		}

		p, err := ParseParams(r.URL.Query(), sc)
		if err != nil {
			var pe paramError
			if errors.As(err, &pe) {
				http.Error(w, pe.msg, http.StatusBadRequest)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		pngR, binR, err := s.render(sc, p)
		if err != nil {
			log.Printf("render error: %v", err)
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		out := pngR
		if binary {
			out = binR
		}

		h := w.Header()
		h.Set("ETag", out.etag)
		// no-cache means "revalidate", not "do not store": the device keeps the
		// bytes and gets a 304 next minute if nothing moved. That is the whole
		// point of the endpoint for something running off a LiPo.
		h.Set("Cache-Control", "private, no-cache, max-age=0, must-revalidate")
		h.Set("X-Panel-Screen", sc.Name)
		h.Set("X-Panel-Width", strconv.Itoa(p.OutW()))
		h.Set("X-Panel-Height", strconv.Itoa(p.OutH()))
		if ifNoneMatch(r.Header.Get("If-None-Match"), out.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if binary {
			h.Set("Content-Type", "application/octet-stream")
			h.Set("X-Panel-Format", "MONO_HLSB")
			h.Set("X-Panel-Stride", strconv.Itoa(p.Stride()))
		} else {
			h.Set("Content-Type", "image/png")
		}
		h.Set("Content-Length", strconv.Itoa(len(out.body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out.body)
	}
}

// ifNoneMatch implements the RFC 9110 comparison for a strong validator.
func ifNoneMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "W/")
		if part == etag {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------- preview

type previewData struct {
	Nav      string
	Screen   Screen
	Screens  []Screen
	Params   Params
	Presets  []preset
	PNGBytes int
	BinBytes int
	Stride   int
	ETag     string
	BinETag  string
	PNGURL   string
	BinURL   string
	Snippet  string
	Now      string
	NowDate  string
	Blocks   int
	MaxArea  int
	MaxDim   int
	SavedAgo string
	Flash    string
	Err      string
	Mismatch bool
}

func (s *Server) currentScreen(r *http.Request) (Screen, bool) {
	if name := strings.TrimSpace(r.URL.Query().Get("s")); name != "" {
		return s.store.Screen(name)
	}
	return s.store.DefaultScreen(), true
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.currentScreen(r)
	if !ok {
		http.Error(w, "no such screen", http.StatusNotFound)
		return
	}
	data := previewData{
		Nav: "preview", Screen: sc, Screens: s.store.Screens(), Presets: presets,
		MaxArea: maxArea, MaxDim: maxDim, Blocks: len(sc.Blocks),
		Flash: r.URL.Query().Get("flash"), Err: r.URL.Query().Get("err"),
	}

	p, err := ParseParams(r.URL.Query(), sc)
	if err != nil {
		var pe paramError
		if errors.As(err, &pe) {
			data.Err = pe.msg
			data.Params = paramsFor(sc)
			s.renderPage(w, r, "preview.html", data)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	pngR, binR, err := s.render(sc, p)
	if err != nil {
		log.Printf("render error: %v", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}

	q := p.Query() + "&k=" + url.QueryEscape(sc.DeviceKey)
	base := "/s/" + url.PathEscape(sc.Name)
	data.Params = p
	data.PNGBytes = len(pngR.body)
	data.BinBytes = len(binR.body)
	data.Stride = p.Stride()
	data.ETag = pngR.etag
	data.BinETag = binR.etag
	data.PNGURL = base + "/screen.png?" + q
	data.BinURL = base + "/screen.bin?" + q
	data.Snippet = micropython(s.baseURL, sc, p)
	data.Now = s.clock().Format("15:04")
	data.NowDate = s.clock().Format("Mon 2 Jan 2006 MST")
	data.Mismatch = p.W != sc.W || p.H != sc.H
	if !sc.UpdatedAt.IsZero() {
		data.SavedAgo = humanize(s.now().Sub(sc.UpdatedAt))
	}
	s.renderPage(w, r, "preview.html", data)
}

// micropython builds the exact fetch-and-blit snippet for the current shape.
// Numbers are baked in so it can be pasted onto a device unchanged.
func micropython(base string, sc Screen, p Params) string {
	path := "/s/" + url.PathEscape(sc.Name) + "/screen.bin"
	u := strings.TrimRight(base, "/") + path + "?" + p.Query() + "&k=" + url.QueryEscape(sc.DeviceKey)
	stride := p.Stride()
	total := p.BinLen()
	ow, oh := p.OutW(), p.OutH()
	var b strings.Builder
	fmt.Fprintf(&b, "# panel -> screen %q, %dx%d, 1bpp MONO_HLSB, MSB first, row-major.\n", sc.Name, ow, oh)
	fmt.Fprintf(&b, "# Bit set (1) = white pixel. Body is always exactly %d bytes\n", total)
	fmt.Fprintf(&b, "# (ceil(%d/8) = %d bytes per row x %d rows), sent with Content-Length,\n", ow, stride, oh)
	b.WriteString("# never chunked, so a raw readinto() into a preallocated buffer is safe.\n\n")
	b.WriteString("import framebuf, urequests\n\n")
	fmt.Fprintf(&b, "URL    = %q\n", u)
	fmt.Fprintf(&b, "W, H   = %d, %d\n", ow, oh)
	fmt.Fprintf(&b, "STRIDE = %d\n", stride)
	fmt.Fprintf(&b, "BUF    = bytearray(STRIDE * H)   # %d bytes\n", total)
	b.WriteString("FB     = framebuf.FrameBuffer(BUF, W, H, framebuf.MONO_HLSB)\n\n")
	b.WriteString("etag = None   # survive deep sleep by stashing this in machine.RTC().memory()\n\n")
	b.WriteString("def refresh(epd):\n")
	b.WriteString("    global etag\n")
	b.WriteString("    hdrs = {\"If-None-Match\": etag} if etag else {}\n")
	b.WriteString("    r = urequests.get(URL, headers=hdrs)\n")
	b.WriteString("    try:\n")
	b.WriteString("        if r.status_code == 304:\n")
	b.WriteString("            return False              # unchanged, go back to sleep\n")
	b.WriteString("        if r.status_code != 200:\n")
	b.WriteString("            raise OSError(\"panel HTTP %d\" % r.status_code)\n")
	b.WriteString("        mv, n = memoryview(BUF), 0\n")
	b.WriteString("        while n < len(BUF):\n")
	b.WriteString("            got = r.raw.readinto(mv[n:])\n")
	b.WriteString("            if not got:\n")
	b.WriteString("                break\n")
	b.WriteString("            n += got\n")
	b.WriteString("        if n != len(BUF):\n")
	b.WriteString("            raise OSError(\"short read %d/%d\" % (n, len(BUF)))\n")
	b.WriteString("        etag = r.headers.get(\"ETag\")\n")
	b.WriteString("    finally:\n")
	b.WriteString("        r.close()\n")
	b.WriteString("    epd.init()\n")
	b.WriteString("    epd.display(BUF)             # or: epd.blit(FB, 0, 0); epd.show()\n")
	b.WriteString("    epd.sleep()\n")
	b.WriteString("    return True\n")
	return b.String()
}

// ----------------------------------------------------------------------- edit

// blockView carries everything one block's form needs. The font list and the
// alignment list are repeated per block rather than reached for through $,
// because inside a {{template}} call $ is the sub-template's own dot.
type blockView struct {
	I      int
	B      Block
	Name   string
	Face   typeface
	Sizes  []int
	Advice string
	Status dataValue
	RowsIn []Row
	Items  string
	Note   string
	Faces  []typeface
	Aligns []string
}

type editData struct {
	Nav       string
	Screen    Screen
	Screens   []Screen
	Views     []blockView
	Faces     []typeface
	Clocks    []namedFormat
	Dates     []namedFormat
	Anchors   []string
	Aligns    []string
	Dithers   []string
	Fits      []string
	Orients   []string
	Types     []string
	TypeName  map[string]string
	Images    []ImageMeta
	Starters  []starter
	PNGURL    string
	Presets   []preset
	JSON      string
	FontRules template.JS
	Focus     int
	Flash     string
	Err       string

	MaxBlocks   int
	MaxScreens  int
	MaxRows     int
	MaxItems    int
	MaxImages   int
	MaxImageMiB int
	MaxTextLen  int
	MaxDim      int
}

func (s *Server) editView(sc Screen, focus int, flash, errMsg, jsonOverride string) editData {
	views := make([]blockView, 0, len(sc.Blocks))
	for i, b := range sc.Blocks {
		b = b.Normalize()
		tf, ok := lookupFace(b.Font)
		if !ok {
			tf = faceByID[defaultFontID]
		}
		v := blockView{
			I: i, B: b, Name: blockTypeNames[b.Type],
			Face: tf, Sizes: tf.LegalSizes(),
			RowsIn: b.Rows, Items: strings.Join(b.Items, "\n"),
			Faces: faces, Aligns: aligns,
		}
		if wantsType(b.Type) {
			v.Advice = tf.SizeAdvice(b.Size)
			v.Note = tf.Note
			if b.Size > sampleCap {
				v.Note += fmt.Sprintf(" Sample shown at %dpx; this block renders at %dpx.", sampleCap, b.Size)
			}
		}
		if b.Type == BlockData {
			v.Status = s.data.Status(b)
		}
		views = append(views, v)
	}
	js := jsonOverride
	if js == "" {
		js = s.store.StateJSON()
	}
	p := paramsFor(sc)
	return editData{
		Nav: "edit", Screen: sc, Screens: s.store.Screens(), Views: views,
		Faces: faces, Clocks: clockFormats, Dates: dateFormats,
		Anchors: anchors, Aligns: aligns, Dithers: dithers, Fits: fits, Orients: orients,
		Types: blockTypes, TypeName: blockTypeNames,
		Images: s.store.Images(), Starters: starters, Presets: presets,
		PNGURL: "/s/" + url.PathEscape(sc.Name) + "/screen.png?" + p.Query() +
			"&k=" + url.QueryEscape(sc.DeviceKey) + "&v=" + sc.Version(),
		JSON: js, FontRules: template.JS(fontRules()), Focus: focus, Flash: flash, Err: errMsg,
		MaxBlocks: maxBlocks, MaxScreens: maxScreens, MaxRows: maxRows, MaxItems: maxListItems,
		MaxImages: maxImages, MaxImageMiB: maxImageBytes >> 20, MaxTextLen: maxTextLen, MaxDim: maxDim,
	}
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.currentScreen(r)
	if !ok {
		http.Error(w, "no such screen", http.StatusNotFound)
		return
	}
	focus, _ := strconv.Atoi(r.URL.Query().Get("b"))
	s.renderPage(w, r, "edit.html", s.editView(sc, focus,
		r.URL.Query().Get("flash"), r.URL.Query().Get("err"), ""))
}

// handleSave takes the whole editor form: screen settings, every block field,
// and one operation. Blocks are parsed first so an add/move/delete never
// discards edits typed into the other rows.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.currentScreen(r)
	if !ok {
		http.Error(w, "no such screen", http.StatusNotFound)
		return
	}
	if err := parseForm(w, r); err != nil {
		http.Error(w, "form too large or malformed", http.StatusBadRequest)
		return
	}

	next := sc
	if n := slug(r.PostFormValue("screen_name")); n != "" {
		next.Name = n
	}
	next.W = formInt(r, "screen_w", sc.W)
	next.H = formInt(r, "screen_h", sc.H)
	next.Rotate = formInt(r, "screen_rotate", sc.Rotate)
	next.Invert = r.PostFormValue("screen_invert") == "1"
	next.Blocks = parseBlocks(r, len(sc.Blocks))

	op, arg, _ := strings.Cut(r.PostFormValue("op"), ".")
	idx, _ := strconv.Atoi(arg)
	focus := idx
	switch op {
	case "add":
		if len(next.Blocks) >= maxBlocks {
			s.editErr(w, r, next, len(next.Blocks)-1, fmt.Sprintf("%d blocks is the cap.", maxBlocks))
			return
		}
		typ := r.PostFormValue("addtype")
		if !contains(blockTypes, typ) {
			typ = BlockText
		}
		next.Blocks = append(next.Blocks, NewBlock(typ, next))
		focus = len(next.Blocks) - 1
	case "dup":
		if idx >= 0 && idx < len(next.Blocks) && len(next.Blocks) < maxBlocks {
			cp := next.Blocks[idx]
			cp.Rows = append([]Row(nil), cp.Rows...)
			cp.Items = append([]string(nil), cp.Items...)
			cp.X, cp.Y = cp.X+8, cp.Y+8
			if cp.X+cp.W > next.W {
				cp.X = next.W - cp.W
			}
			if cp.Y+cp.H > next.H {
				cp.Y = next.H - cp.H
			}
			next.Blocks = append(next.Blocks, Block{})
			copy(next.Blocks[idx+2:], next.Blocks[idx+1:])
			next.Blocks[idx+1] = cp
			focus = idx + 1
		}
	case "del":
		if idx >= 0 && idx < len(next.Blocks) {
			next.Blocks = append(next.Blocks[:idx], next.Blocks[idx+1:]...)
			focus = idx - 1
		}
	case "up":
		if idx > 0 && idx < len(next.Blocks) {
			next.Blocks[idx-1], next.Blocks[idx] = next.Blocks[idx], next.Blocks[idx-1]
			focus = idx - 1
		}
	case "down":
		if idx >= 0 && idx < len(next.Blocks)-1 {
			next.Blocks[idx+1], next.Blocks[idx] = next.Blocks[idx], next.Blocks[idx+1]
			focus = idx + 1
		}
	}

	for i := range next.Blocks {
		next.Blocks[i] = next.Blocks[i].Normalize()
	}

	if err := s.store.SaveScreen(sc.Name, next); err != nil {
		s.editErr(w, r, next, focus, err.Error())
		return
	}
	s.data.RefreshNow(s.ctx, next.DataBlocks())
	s.redirectEdit(w, r, next.Name, focus, "Saved.", "")
}

// editErr re-renders the editor with the user's unsaved work intact and the
// precise problem named.
func (s *Server) editErr(w http.ResponseWriter, r *http.Request, sc Screen, focus int, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	s.renderPage(w, r, "edit.html", s.editView(sc, focus, "", msg, ""))
}

func (s *Server) redirectEdit(w http.ResponseWriter, r *http.Request, name string, focus int, flash, errMsg string) {
	q := url.Values{}
	q.Set("s", name)
	if focus >= 0 {
		q.Set("b", strconv.Itoa(focus))
	}
	if flash != "" {
		q.Set("flash", flash)
	}
	if errMsg != "" {
		q.Set("err", errMsg)
	}
	target := "/edit?" + q.Encode()
	if focus >= 0 {
		target += "#block-" + strconv.Itoa(focus)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// parseBlocks reads the indexed b{i}.field form values back into blocks.
func parseBlocks(r *http.Request, hint int) []Block {
	n := hint
	for i := 0; i < maxBlocks; i++ {
		if r.PostForm.Has(fmt.Sprintf("b%d.type", i)) && i+1 > n {
			n = i + 1
		}
	}
	out := make([]Block, 0, n)
	for i := 0; i < n; i++ {
		key := func(f string) string { return fmt.Sprintf("b%d.%s", i, f) }
		if !r.PostForm.Has(key("type")) {
			continue
		}
		b := Block{
			Type:      r.PostFormValue(key("type")),
			X:         formInt(r, key("x"), 0),
			Y:         formInt(r, key("y"), 0),
			W:         formInt(r, key("w"), 0),
			H:         formInt(r, key("h"), 0),
			Anchor:    r.PostFormValue(key("anchor")),
			Text:      r.PostFormValue(key("text")),
			Font:      r.PostFormValue(key("font")),
			Size:      formInt(r, key("size"), 0),
			Align:     r.PostFormValue(key("align")),
			Wrap:      r.PostFormValue(key("wrap")) == "1",
			Invert:    r.PostFormValue(key("invert")) == "1",
			Format:    r.PostFormValue(key("format")),
			Rules:     r.PostFormValue(key("rules")) == "1",
			Bullets:   r.PostFormValue(key("bullets")) == "1",
			Orient:    r.PostFormValue(key("orient")),
			Thickness: formInt(r, key("thickness"), 0),
			Fill:      r.PostFormValue(key("fill")) == "1",
			Label:     r.PostFormValue(key("label")),
			Value:     formFloat(r, key("value"), 0),
			Image:     r.PostFormValue(key("image")),
			Dither:    r.PostFormValue(key("dither")),
			Fit:       r.PostFormValue(key("fit")),
			URL:       r.PostFormValue(key("url")),
			Path:      r.PostFormValue(key("path")),
			Interval:  formInt(r, key("interval"), 0),
			Timeout:   formInt(r, key("timeout"), 0),
			Fallback:  r.PostFormValue(key("fallback")),
		}
		if b.Type == BlockRows {
			labels := r.PostForm[key("rowlabel")]
			values := r.PostForm[key("rowvalue")]
			for j := range labels {
				v := ""
				if j < len(values) {
					v = values[j]
				}
				if strings.TrimSpace(labels[j]) == "" && strings.TrimSpace(v) == "" {
					continue
				}
				b.Rows = append(b.Rows, Row{Label: labels[j], Value: v})
				if len(b.Rows) == maxRows {
					break
				}
			}
		}
		if b.Type == BlockList {
			for _, line := range strings.Split(r.PostFormValue(key("items")), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				b.Items = append(b.Items, line)
				if len(b.Items) == maxListItems {
					break
				}
			}
		}
		out = append(out, b)
	}
	return out
}

func formInt(r *http.Request, key string, def int) int {
	raw := strings.TrimSpace(r.PostFormValue(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func formFloat(r *http.Request, key string, def float64) float64 {
	raw := strings.TrimSpace(r.PostFormValue(key))
	if raw == "" {
		return def
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return n
}

func parseForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	return r.ParseForm()
}

// ------------------------------------------------------------------- screens

func (s *Server) handleScreens(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, "form too large or malformed", http.StatusBadRequest)
		return
	}
	current := r.PostFormValue("s")
	switch r.PostFormValue("op") {
	case "add":
		name := r.PostFormValue("name")
		w0 := formInt(r, "w", 800)
		h0 := formInt(r, "h", 480)
		sc, err := s.store.AddScreen(name, w0, h0)
		if err != nil {
			s.redirectEdit(w, r, current, -1, "", err.Error())
			return
		}
		s.redirectEdit(w, r, sc.Name, -1, "Screen added.", "")
	case "duplicate":
		sc, err := s.store.DuplicateScreen(current)
		if err != nil {
			s.redirectEdit(w, r, current, -1, "", err.Error())
			return
		}
		s.redirectEdit(w, r, sc.Name, -1, "Screen duplicated.", "")
	case "delete":
		if err := s.store.DeleteScreen(current); err != nil {
			s.redirectEdit(w, r, current, -1, "", err.Error())
			return
		}
		s.redirectEdit(w, r, s.store.DefaultScreen().Name, -1, "Screen deleted.", "")
	default:
		http.Error(w, "unknown operation", http.StatusBadRequest)
	}
}

func (s *Server) handleStarter(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, "form too large or malformed", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("s")
	sc, ok := s.store.Screen(name)
	if !ok {
		http.Error(w, "no such screen", http.StatusNotFound)
		return
	}
	id := r.PostFormValue("starter")
	st, ok := starterByID(id)
	if !ok {
		s.redirectEdit(w, r, name, -1, "", "unknown starter layout "+strconv.Quote(id))
		return
	}
	sc.Blocks = starterBlocks(id, sc)
	// The photo starter needs a picture; use the first uploaded one if there is
	// one, otherwise drop the block rather than save something invalid.
	if imgs := s.store.Images(); len(imgs) > 0 {
		for i := range sc.Blocks {
			if sc.Blocks[i].Type == BlockImage && sc.Blocks[i].Image == "" {
				sc.Blocks[i].Image = imgs[0].ID
			}
		}
	} else {
		kept := sc.Blocks[:0]
		for _, b := range sc.Blocks {
			if b.Type == BlockImage && b.Image == "" {
				continue
			}
			kept = append(kept, b)
		}
		sc.Blocks = kept
	}
	if err := s.store.SaveScreen(name, sc); err != nil {
		s.redirectEdit(w, r, name, -1, "", err.Error())
		return
	}
	s.data.RefreshNow(s.ctx, sc.DataBlocks())
	s.redirectEdit(w, r, name, -1, st.Name+" loaded.", "")
}

func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, "form too large or malformed", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("s")
	if _, err := s.store.RotateKey(name); err != nil {
		s.redirectEdit(w, r, name, -1, "", err.Error())
		return
	}
	s.redirectEdit(w, r, name, -1, "Device key rotated. Every old device URL is now dead.", "")
}

// ------------------------------------------------------------- layout as JSON

// handleLayoutSave is the escape hatch: paste the whole document. Validation
// failures come back naming the block and the field, with the submitted text
// still in the textarea.
func (s *Server) handleLayoutSave(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, "form too large or malformed", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("s")
	sc, ok := s.store.Screen(name)
	if !ok {
		sc = s.store.DefaultScreen()
	}
	body := r.PostFormValue("json")

	var next State
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&next); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.renderPage(w, r, "edit.html", s.editView(sc, -1, "", "layout JSON: "+jsonErr(err), body))
		return
	}
	if err := s.store.ReplaceState(next); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.renderPage(w, r, "edit.html", s.editView(sc, -1, "", err.Error(), body))
		return
	}
	target := s.store.DefaultScreen()
	for _, cand := range s.store.Screens() {
		if cand.Name == name {
			target = cand
		}
	}
	var blocks []Block
	for _, cand := range s.store.Screens() {
		blocks = append(blocks, cand.DataBlocks()...)
	}
	s.data.RefreshNow(s.ctx, blocks)
	s.redirectEdit(w, r, target.Name, -1, "Layout replaced.", "")
}

// jsonErr turns encoding/json's errors into something a person can act on.
func jsonErr(err error) string {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "the document ends part-way through; a bracket or brace is unclosed"
	}
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return fmt.Sprintf("syntax error at byte %d: %v", syn.Offset, syn)
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		where := typ.Field
		if where == "" {
			where = typ.Struct
		}
		if where == "" {
			where = "the document"
		}
		return fmt.Sprintf("%q wants a %s, got %s (byte %d)", where, typ.Type, typ.Value, typ.Offset)
	}
	return err.Error()
}

func (s *Server) handleLayoutJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(s.store.StateJSON()))
}

// -------------------------------------------------------------------- images

func (s *Server) handleImageUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+(64<<10))
	name := r.URL.Query().Get("s")
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		s.redirectEdit(w, r, name, -1, "", fmt.Sprintf("upload failed: over the %d MiB cap or malformed.", maxImageBytes>>20))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	if v := r.PostFormValue("s"); v != "" {
		name = v
	}

	file, hdr, err := r.FormFile("file")
	if err != nil {
		s.redirectEdit(w, r, name, -1, "", "no file was attached.")
		return
	}
	defer file.Close()

	if len(s.store.Images()) >= maxImages {
		s.redirectEdit(w, r, name, -1, "", fmt.Sprintf("%d images is the cap; delete one first.", maxImages))
		return
	}

	id, err := newID()
	if err != nil {
		log.Printf("id error: %v", err)
		http.Error(w, "could not store image", http.StatusInternalServerError)
		return
	}
	meta, err := s.images.Put(id, file)
	if err != nil {
		s.redirectEdit(w, r, name, -1, "", err.Error())
		return
	}
	meta.Name = clean(hdr.Filename, 64)
	if meta.Name == "" {
		meta.Name = "upload"
	}
	meta.CreatedAt = time.Now().UTC()
	if err := s.store.AddImage(meta); err != nil {
		_ = s.images.Remove(id)
		s.redirectEdit(w, r, name, -1, "", err.Error())
		return
	}
	s.redirectEdit(w, r, name, -1, fmt.Sprintf("Uploaded %s, %dx%d.", meta.Name, meta.W, meta.H), "")
}

func (s *Server) handleImageDelete(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, "form too large or malformed", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("s")
	id := r.PostFormValue("id")
	if err := s.store.DeleteImage(id); err != nil {
		s.redirectEdit(w, r, name, -1, "", err.Error())
		return
	}
	if err := s.images.Remove(id); err != nil {
		log.Printf("image remove: %v", err)
	}
	s.redirectEdit(w, r, name, -1, "Image deleted.", "")
}

// handleImageServe returns the stored greyscale PNG for the editor thumbnails.
// Token gated: an uploaded photo is not public.
func (s *Server) handleImageServe(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.store.ImageIDs()[id] {
		http.NotFound(w, r)
		return
	}
	b, err := s.images.PNG(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	h.Set("Cache-Control", "private, max-age=300")
	h.Set("Content-Disposition", "inline")
	_, _ = w.Write(b)
}

// handleSample renders the font/size sample strip shown in the editor. It goes
// through the real renderer, so what you preview is what a panel gets.
func (s *Server) handleSample(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("font")
	if _, ok := lookupFace(id); !ok {
		id = defaultFontID
	}
	size, _ := strconv.Atoi(q.Get("size"))
	img := SampleStrip(id, size)
	body, err := EncodePNG(img)
	if err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(body)
}

// fontRules is the size policy handed to the editor as JSON so the size
// control can reconfigure itself the instant the font changes, using exactly
// the same numbers the server validates against.
type fontRule struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Bitmap bool   `json:"bitmap"`
	Cap    int    `json:"cap"`
	Sizes  []int  `json:"sizes,omitempty"`
	Min    int    `json:"min,omitempty"`
	Max    int    `json:"max,omitempty"`
	Alt    string `json:"alt,omitempty"`
	AltAt  int    `json:"altAt,omitempty"`
	Note   string `json:"note"`
}

func fontRules() string {
	out := make([]fontRule, 0, len(faces))
	for _, f := range faces {
		r := fontRule{ID: f.ID, Name: f.Name, Bitmap: f.Bitmap, Note: f.Note, Cap: sampleCap}
		if f.Bitmap {
			r.Sizes = f.LegalSizes()
		} else {
			r.Min, r.Max, r.Alt, r.AltAt = f.Min, f.Max, f.Alt, f.AltSize
		}
		out = append(out, r)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ---------------------------------------------------------------------- login

type loginData struct {
	Nav   string
	Flash string
	Err   string
	Next  string
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authed(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderPage(w, r, "login.html", loginData{Nav: "login", Next: safeNext(r.URL.Query().Get("next"))})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}
	ip := clientIP(r)
	if s.auth.limiter.Blocked(ip, time.Now()) {
		w.WriteHeader(http.StatusTooManyRequests)
		s.renderPage(w, r, "login.html", loginData{Nav: "login", Err: "Too many attempts. Wait a minute."})
		return
	}
	if !s.auth.tokenMatches(r.PostFormValue("token")) {
		s.auth.limiter.Fail(ip, time.Now())
		w.WriteHeader(http.StatusUnauthorized)
		s.renderPage(w, r, "login.html", loginData{Nav: "login", Err: "Rejected.", Next: safeNext(r.PostFormValue("next"))})
		return
	}
	sess, err := s.auth.NewSession()
	if err != nil {
		log.Printf("session error: %v", err)
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	s.auth.SetSessionCookie(w, sess)
	http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// safeNext keeps redirects on this origin.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

// ------------------------------------------------------------------ middleware

// requireToken gates the HTML surface. Browsers get bounced to the login form;
// anything else gets a flat 401.
func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth.Authed(r) {
			next(w, r)
			return
		}
		ip := clientIP(r)
		if s.auth.limiter.Blocked(ip, time.Now()) {
			http.Error(w, "too many failed attempts, try again in a minute", http.StatusTooManyRequests)
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		s.auth.limiter.Fail(ip, time.Now())
		w.Header().Set("WWW-Authenticate", `Bearer realm="panel"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

// requireSameSite is the CSRF gate on every state-changing endpoint.
func requireSameSite(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameSiteRequest(r) {
			http.Error(w, "cross-site form posts are refused", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// humanize renders a duration the way a person would say it.
func humanize(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Minute:
		return "1 minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "1 hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/24/7))
	}
}
