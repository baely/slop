package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
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
	png  *image.Paletted
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
	cache   *renderCache
	loc     *time.Location
	baseURL string
	now     func() time.Time
}

func (s *Server) clock() time.Time { return s.now().In(s.loc).Truncate(time.Minute) }

// render produces (and caches) the artefacts for one request shape.
func (s *Server) render(cfg Config, p Params) (*rendered, *rendered, error) {
	now := s.clock()
	key := fmt.Sprintf("%s|%s|%s", p.Query(), now.Format(time.RFC3339), cfg.Version())
	if pngR, ok := s.cache.get("png|" + key); ok {
		if binR, ok := s.cache.get("bin|" + key); ok {
			return pngR, binR, nil
		}
	}
	img := Render(cfg, now, p)
	body, err := EncodePNG(img)
	if err != nil {
		return nil, nil, err
	}
	pngR := &rendered{body: body, etag: etagOf(body), png: img}
	bin := PackBits(img)
	binR := &rendered{body: bin, etag: etagOf(bin), png: img}
	s.cache.put("png|"+key, pngR)
	s.cache.put("bin|"+key, binR)
	return pngR, binR, nil
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// ---------------------------------------------------------------- device auth

// deviceAuthed accepts the unguessable device key (query `k` or bearer), the
// app token as a bearer, or a signed session cookie so the preview <img> loads.
func (s *Server) deviceAuthed(r *http.Request, cfg Config) bool {
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
	return subtle.ConstantTimeCompare([]byte(cand), []byte(cfg.DeviceKey)) == 1
}

// ------------------------------------------------------------------- /screen.*

func (s *Server) handleScreen(binary bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.store.Get()
		ip := clientIP(r)
		if s.auth.limiter.Blocked(ip, time.Now()) {
			http.Error(w, "too many failed attempts, try again in a minute", http.StatusTooManyRequests)
			return
		}
		if !s.deviceAuthed(r, cfg) {
			s.auth.limiter.Fail(ip, time.Now())
			w.Header().Set("WWW-Authenticate", `Bearer realm="panel"`)
			http.Error(w, "this panel needs its device key: add ?k=<key> or an Authorization: Bearer header. The key is on the preview page.", http.StatusUnauthorized)
			return
		}

		p, err := ParseParams(r.URL.Query())
		if err != nil {
			var pe paramError
			if errors.As(err, &pe) {
				http.Error(w, pe.msg, http.StatusBadRequest)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		pngR, binR, err := s.render(cfg, p)
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
		h.Set("X-Panel-Width", strconv.Itoa(p.W))
		h.Set("X-Panel-Height", strconv.Itoa(p.H))
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
	Nav       string
	Params    Params
	Config    Config
	Presets   []preset
	PNGBytes  int
	BinBytes  int
	Stride    int
	ETag      string
	BinETag   string
	PNGURL    string
	BinURL    string
	Snippet   string
	Now       string
	NowDate   string
	MaxRows   int
	MaxArea   int
	MaxDim    int
	SavedAgo  string
	Flash     string
	DeviceKey string
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	p, err := ParseParams(r.URL.Query())
	if err != nil {
		var pe paramError
		if errors.As(err, &pe) {
			s.renderPage(w, r, "preview.html", previewData{Nav: "preview", Flash: pe.msg, Config: cfg,
				Params: defaultParams(), Presets: presets, MaxRows: maxRows, MaxArea: maxArea, MaxDim: maxDim,
				DeviceKey: cfg.DeviceKey})
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	pngR, binR, err := s.render(cfg, p)
	if err != nil {
		log.Printf("render error: %v", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}

	q := p.Query() + "&k=" + url.QueryEscape(cfg.DeviceKey)
	data := previewData{
		Nav:       "preview",
		Params:    p,
		Config:    cfg,
		Presets:   presets,
		PNGBytes:  len(pngR.body),
		BinBytes:  len(binR.body),
		Stride:    p.Stride(),
		ETag:      pngR.etag,
		BinETag:   binR.etag,
		PNGURL:    "/screen.png?" + q,
		BinURL:    "/screen.bin?" + q,
		Snippet:   micropython(s.baseURL, p, cfg.DeviceKey),
		Now:       s.clock().Format("15:04"),
		NowDate:   s.clock().Format("Mon 2 Jan 2006 MST"),
		MaxRows:   maxRows,
		MaxArea:   maxArea,
		MaxDim:    maxDim,
		DeviceKey: cfg.DeviceKey,
		Flash:     r.URL.Query().Get("flash"),
	}
	if !cfg.UpdatedAt.IsZero() {
		data.SavedAgo = humanize(s.now().Sub(cfg.UpdatedAt))
	}
	s.renderPage(w, r, "preview.html", data)
}

// micropython builds the exact fetch-and-blit snippet for the current shape.
// Numbers are baked in so it can be pasted onto a device unchanged.
func micropython(base string, p Params, key string) string {
	u := strings.TrimRight(base, "/") + "/screen.bin?" + p.Query() + "&k=" + url.QueryEscape(key)
	stride := p.Stride()
	total := p.BinLen()
	var b strings.Builder
	fmt.Fprintf(&b, "# panel -> %dx%d, 1bpp MONO_HLSB, MSB first, row-major.\n", p.W, p.H)
	fmt.Fprintf(&b, "# Bit set (1) = white pixel. Body is always exactly %d bytes\n", total)
	fmt.Fprintf(&b, "# (ceil(%d/8) = %d bytes per row x %d rows), sent with Content-Length,\n", p.W, stride, p.H)
	b.WriteString("# never chunked, so a raw readinto() into a preallocated buffer is safe.\n\n")
	b.WriteString("import framebuf, urequests\n\n")
	fmt.Fprintf(&b, "URL    = %q\n", u)
	fmt.Fprintf(&b, "W, H   = %d, %d\n", p.W, p.H)
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

type editData struct {
	Nav          string
	Config       Config
	Blanks       []int
	MaxRows      int
	MaxTitleLen  int
	MaxLabelLen  int
	MaxValueLen  int
	MaxFooterLen int
	Flash        string
	DeviceKey    string
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	blanks := 6
	if n := maxRows - len(cfg.Rows); n < blanks {
		blanks = n
	}
	if blanks < 0 {
		blanks = 0
	}
	s.renderPage(w, r, "edit.html", editData{
		Nav: "edit", Config: cfg, Blanks: make([]int, blanks),
		MaxRows: maxRows, MaxTitleLen: maxTitleLen, MaxLabelLen: maxLabelLen,
		MaxValueLen: maxValueLen, MaxFooterLen: maxFooterLen,
		Flash: r.URL.Query().Get("flash"), DeviceKey: cfg.DeviceKey,
	})
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or malformed", http.StatusBadRequest)
		return
	}
	labels := r.PostForm["label"]
	values := r.PostForm["value"]
	rows := make([]Row, 0, len(labels))
	for i := range labels {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		rows = append(rows, Row{Label: labels[i], Value: v})
	}
	if err := s.store.Save(r.PostFormValue("title"), rows, r.PostFormValue("footer")); err != nil {
		log.Printf("save error: %v", err)
		http.Error(w, "could not save", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/edit?flash=Saved.", http.StatusSeeOther)
}

func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if _, err := s.store.RotateKey(); err != nil {
		log.Printf("rotate error: %v", err)
		http.Error(w, "could not rotate key", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/edit?flash=Device+key+rotated.+Every+old+device+URL+is+now+dead.", http.StatusSeeOther)
}

// ---------------------------------------------------------------------- login

type loginData struct {
	Nav   string
	Flash string
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
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}
	ip := clientIP(r)
	if s.auth.limiter.Blocked(ip, time.Now()) {
		w.WriteHeader(http.StatusTooManyRequests)
		s.renderPage(w, r, "login.html", loginData{Nav: "login", Flash: "Too many attempts. Wait a minute."})
		return
	}
	if !s.auth.tokenMatches(r.PostFormValue("token")) {
		s.auth.limiter.Fail(ip, time.Now())
		w.WriteHeader(http.StatusUnauthorized)
		s.renderPage(w, r, "login.html", loginData{Nav: "login", Flash: "Rejected.", Next: safeNext(r.PostFormValue("next"))})
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
