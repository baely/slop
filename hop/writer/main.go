// Command hop-writer edits hop's links file through a small internal web
// page: list, add, change and delete links, and mint random keys from
// character rules. It writes the same file hop watches.
package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/baely/slop/hop/links"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	path := flag.String("links", "/data/links.txt", "links file shared with hop")
	base := flag.String("base", "https://bly.au", "public base URL of hop, for display")
	flag.Parse()
	logger := log.New(os.Stderr, "", 0)

	h := &handler{store: &store{path: *path}, base: strings.TrimRight(*base, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.index)
	mux.HandleFunc("POST /add", h.add)
	mux.HandleFunc("POST /update", h.update)
	mux.HandleFunc("POST /delete", h.delete)
	mux.HandleFunc("GET /generate", h.generate)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	logger.Printf("hop-writer: editing %s, listening on %s", *path, *addr)
	logger.Fatal(srv.ListenAndServe())
}

type handler struct {
	store *store
	base  string
}

type entry struct {
	Key     string // file key, "" for the root
	Display string // bly.au/key
	Short   string // https://bly.au/key
	URL     string
}

type form struct {
	Key   string
	URL   string
	Rules rules
}

type pageData struct {
	Links []entry
	Form  form
	Error string
}

func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, form{Rules: defaultRules}, "")
}

func (h *handler) add(w http.ResponseWriter, r *http.Request) {
	f := form{Key: strings.TrimSpace(r.FormValue("key")), URL: strings.TrimSpace(r.FormValue("url")), Rules: rulesFrom(r.Form)}
	if f.Key == "" {
		taken, err := h.taken()
		if err != nil {
			h.render(w, http.StatusInternalServerError, f, err.Error())
			return
		}
		f.Key, err = generate(f.Rules, taken)
		if err != nil {
			h.render(w, http.StatusBadRequest, f, err.Error())
			return
		}
	}
	if err := h.store.Add(f.Key, f.URL); err != nil {
		h.render(w, http.StatusBadRequest, f, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Update(r.FormValue("key"), strings.TrimSpace(r.FormValue("url"))); err != nil {
		h.render(w, http.StatusBadRequest, form{Rules: defaultRules}, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.FormValue("key")); err != nil {
		h.render(w, http.StatusBadRequest, form{Rules: defaultRules}, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// generate answers the page's Generate button with one fresh key as text.
func (h *handler) generate(w http.ResponseWriter, r *http.Request) {
	taken, err := h.taken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	k, err := generate(rulesFrom(r.URL.Query()), taken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(k))
}

// taken reports which keys are already in the file.
func (h *handler) taken() (func(string) bool, error) {
	ls, err := h.store.List()
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(ls))
	for _, l := range ls {
		have[l.Key()] = true
	}
	return func(k string) bool { return have[k] }, nil
}

func (h *handler) render(w http.ResponseWriter, status int, f form, errMsg string) {
	ls, err := h.store.List()
	if err != nil && errMsg == "" {
		errMsg, status = err.Error(), http.StatusInternalServerError
	}
	data := pageData{Form: f, Error: errMsg}
	for _, l := range ls {
		data.Links = append(data.Links, h.entry(l))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := page.Execute(w, data); err != nil {
		log.Printf("hop-writer: render: %v", err)
	}
}

func (h *handler) entry(l links.Link) entry {
	host := h.base
	if u, err := url.Parse(h.base); err == nil && u.Host != "" {
		host = u.Host
	}
	return entry{Key: l.Key(), Display: host + l.Path, Short: h.base + l.Path, URL: l.URL}
}

// rulesFrom reads the rule fields from a form or query. A request carrying
// no rule fields at all gets the defaults.
func rulesFrom(v url.Values) rules {
	if v.Get("len") == "" && v.Get("lower") == "" && v.Get("upper") == "" && v.Get("digits") == "" && v.Get("unambiguous") == "" {
		return defaultRules
	}
	n, _ := strconv.Atoi(v.Get("len"))
	return rules{
		Length:      n,
		Lower:       v.Get("lower") != "",
		Upper:       v.Get("upper") != "",
		Digits:      v.Get("digits") != "",
		Unambiguous: v.Get("unambiguous") != "",
	}
}
