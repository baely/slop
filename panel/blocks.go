package main

import (
	"fmt"
	"net/url"
	"strings"
)

// Block types. A screen is a list of these, drawn in order, so a later block
// draws over an earlier one.
const (
	BlockText     = "text"
	BlockClock    = "clock"
	BlockDate     = "date"
	BlockRows     = "rows"
	BlockList     = "list"
	BlockDivider  = "divider"
	BlockBox      = "box"
	BlockProgress = "progress"
	BlockImage    = "image"
	BlockData     = "data"
)

var blockTypes = []string{
	BlockText, BlockClock, BlockDate, BlockRows, BlockList,
	BlockDivider, BlockBox, BlockProgress, BlockImage, BlockData,
}

var blockTypeNames = map[string]string{
	BlockText:     "Text",
	BlockClock:    "Clock",
	BlockDate:     "Date",
	BlockRows:     "Rows",
	BlockList:     "List",
	BlockDivider:  "Divider",
	BlockBox:      "Box",
	BlockProgress: "Progress",
	BlockImage:    "Image",
	BlockData:     "Data",
}

var anchors = []string{"nw", "n", "ne", "w", "c", "e", "sw", "s", "se"}
var aligns = []string{"left", "center", "right"}
var dithers = []string{"floyd", "atkinson", "threshold"}
var fits = []string{"contain", "cover", "stretch"}
var orients = []string{"horizontal", "vertical"}

// clockFormats and dateFormats are named, not strftime archaeology.
var clockFormats = []namedFormat{
	{"24h", "24 Hour · 14:32", "15:04", false},
	{"24h-sec", "24 Hour + Seconds · 14:32:07", "15:04:05", true},
	{"12h", "12 Hour · 2:32 pm", "3:04 pm", false},
	{"12h-sec", "12 Hour + Seconds · 2:32:07 pm", "3:04:05 pm", true},
	{"12h-short", "12 Hour, No Suffix · 2:32", "3:04", false},
}

var dateFormats = []namedFormat{
	{"long", "Long · Saturday, 25 July 2026", "Monday, 2 January 2006", false},
	{"medium", "Medium · Sat 25 Jul 2026", "Mon 2 Jan 2006", false},
	{"short", "Short · 25/07/2026", "02/01/2006", false},
	{"iso", "ISO · 2026-07-25", "2006-01-02", false},
	{"weekday", "Weekday · Saturday", "Monday", false},
	{"daymonth", "Day and Month · 25 July", "2 January", false},
}

type namedFormat struct {
	ID      string
	Label   string
	Layout  string
	Seconds bool
}

func findFormat(list []namedFormat, id string) (namedFormat, bool) {
	for _, f := range list {
		if f.ID == id {
			return f, true
		}
	}
	return namedFormat{}, false
}

// Row is one label/value line inside a rows block.
type Row struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Block is deliberately one flat struct rather than a per-type union. The
// alternative is a json.RawMessage payload plus ten decoders, which buys
// nothing here and makes both the HTML form and the JSON escape hatch worse to
// read. Fields not relevant to a type are omitted from the JSON and ignored by
// the renderer.
type Block struct {
	Type   string `json:"type"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Anchor string `json:"anchor,omitempty"`

	// text, clock, date, data, list, rows, progress
	Text   string `json:"text,omitempty"`
	Font   string `json:"font,omitempty"`
	Size   int    `json:"size,omitempty"`
	Align  string `json:"align,omitempty"`
	Wrap   bool   `json:"wrap,omitempty"`
	Invert bool   `json:"invert,omitempty"`

	// clock, date
	Format string `json:"format,omitempty"`

	// rows
	Rows  []Row `json:"rows,omitempty"`
	Rules bool  `json:"rules,omitempty"`

	// list
	Items   []string `json:"items,omitempty"`
	Bullets bool     `json:"bullets,omitempty"`

	// divider, box
	Orient    string `json:"orient,omitempty"`
	Thickness int    `json:"thickness,omitempty"`
	Fill      bool   `json:"fill,omitempty"`

	// progress
	Label string  `json:"label,omitempty"`
	Value float64 `json:"value,omitempty"`

	// image
	Image  string `json:"image,omitempty"`
	Dither string `json:"dither,omitempty"`
	Fit    string `json:"fit,omitempty"`

	// data
	URL      string `json:"url,omitempty"`
	Path     string `json:"path,omitempty"`
	Interval int    `json:"interval,omitempty"`
	Timeout  int    `json:"timeout,omitempty"`
	Fallback string `json:"fallback,omitempty"`
}

// Caps. Stated in the UI, enforced here.
const (
	maxScreens    = 12
	maxBlocks     = 48
	maxRows       = 24
	maxListItems  = 24
	maxTextLen    = 480
	maxLabelLen   = 48
	maxValueLen   = 48
	maxNameLen    = 32
	maxURLLen     = 512
	maxPathLen    = 128
	maxDataBlocks = 8

	minInterval     = 30
	maxInterval     = 86400
	defaultInterval = 300
	minTimeout      = 1
	maxTimeout      = 15
	defaultTimeout  = 5

	maxRequestBody = 512 << 10
)

// NewBlock returns a sensible starting block of the given type, sized for the
// screen it is being dropped onto. Defaults are opinionated on purpose: a big
// clock gets an outline face at a large size, dense rows and lists get a
// bitmap face at its native size, because that is what survives 1-bit.
func NewBlock(typ string, sc Screen) Block {
	b := Block{Type: typ, X: 16, Y: 16, Anchor: "nw", Align: "left"}
	switch typ {
	case BlockText:
		b.Text = "Text"
		b.Font, b.Size = "go-bold", 28
		b.W, b.H = clampTo(sc.W-32, 240), 40
	case BlockClock:
		b.Format = "24h"
		b.Font, b.Size = "go-bold", 96
		b.W, b.H = clampTo(sc.W-32, 320), 112
	case BlockDate:
		b.Format = "medium"
		b.Font, b.Size = "go", 24
		b.W, b.H = clampTo(sc.W-32, 280), 32
	case BlockRows:
		b.Rows = []Row{{"Bin Night", "Tuesday"}, {"Battery", "3.94 V"}}
		b.Font, b.Size = "pixel8x16", 16
		b.Rules = true
		b.W, b.H = clampTo(sc.W-32, 360), 80
	case BlockList:
		b.Items = []string{"First", "Second", "Third"}
		b.Font, b.Size = "pixel8x16", 16
		b.Bullets = true
		b.W, b.H = clampTo(sc.W-32, 300), 72
	case BlockDivider:
		b.Orient, b.Thickness = "horizontal", 2
		b.W, b.H = clampTo(sc.W-32, 400), 2
	case BlockBox:
		b.Thickness = 2
		b.W, b.H = clampTo(sc.W-32, 200), 80
	case BlockProgress:
		b.Label = "Progress"
		b.Value = 62
		b.Font, b.Size = "pixel8x16", 16
		b.W, b.H = clampTo(sc.W-32, 320), 44
	case BlockImage:
		b.Dither, b.Fit, b.Anchor = "floyd", "contain", "c"
		b.W, b.H = clampTo(sc.W-32, 320), clampTo(sc.H-32, 240)
	case BlockData:
		b.URL = "https://example.com/data.json"
		b.Path = "value"
		b.Text = "{{v}}"
		b.Fallback = "—"
		b.Interval, b.Timeout = defaultInterval, defaultTimeout
		b.Font, b.Size = "go-bold", 32
		b.W, b.H = clampTo(sc.W-32, 260), 44
	}
	return b
}

func clampTo(avail, want int) int {
	if avail < 8 {
		avail = 8
	}
	if want > avail {
		return avail
	}
	return want
}

// blockError names the offending block, its type, the field and the problem.
// Every validation failure is one of these; there is no generic path.
type blockError struct {
	Index int
	Type  string
	Field string
	Msg   string
}

func (e blockError) Error() string {
	head := fmt.Sprintf("block %d", e.Index+1)
	if e.Type != "" {
		head += " (" + e.Type + ")"
	}
	if e.Field != "" {
		return fmt.Sprintf("%s: %q %s", head, e.Field, e.Msg)
	}
	return head + ": " + e.Msg
}

func berr(i int, typ, field, format string, args ...any) blockError {
	return blockError{Index: i, Type: typ, Field: field, Msg: fmt.Sprintf(format, args...)}
}

// Normalize fills in defaults for omitted fields and trims strings. It never
// silently corrects a value the user explicitly set to something wrong — that
// is Validate's job, so the error can name it.
func (b Block) Normalize() Block {
	b.Type = strings.ToLower(strings.TrimSpace(b.Type))
	b.Anchor = strings.ToLower(strings.TrimSpace(b.Anchor))
	if b.Anchor == "" {
		b.Anchor = "nw"
	}
	b.Align = strings.ToLower(strings.TrimSpace(b.Align))
	if b.Align == "" {
		b.Align = "left"
	}
	b.Font = strings.TrimSpace(b.Font)
	b.Text = cleanMulti(b.Text, maxTextLen)
	b.Label = clean(b.Label, maxLabelLen)
	b.Fallback = clean(b.Fallback, maxValueLen)

	switch b.Type {
	case BlockClock:
		if b.Format == "" {
			b.Format = "24h"
		}
		if b.Font == "" {
			b.Font, b.Size = "go-bold", 96
		}
	case BlockDate:
		if b.Format == "" {
			b.Format = "medium"
		}
		if b.Font == "" {
			b.Font, b.Size = "go", 24
		}
	case BlockDivider:
		if b.Orient == "" {
			b.Orient = "horizontal"
		}
		if b.Thickness == 0 {
			b.Thickness = 2
		}
	case BlockBox:
		if b.Thickness == 0 && !b.Fill {
			b.Thickness = 2
		}
	case BlockImage:
		if b.Dither == "" {
			b.Dither = "floyd"
		}
		if b.Fit == "" {
			b.Fit = "contain"
		}
	case BlockData:
		if b.Text == "" {
			b.Text = "{{v}}"
		}
		if b.Fallback == "" {
			b.Fallback = "—"
		}
		if b.Interval == 0 {
			b.Interval = defaultInterval
		}
		if b.Timeout == 0 {
			b.Timeout = defaultTimeout
		}
		b.URL = strings.TrimSpace(b.URL)
		b.Path = strings.TrimSpace(b.Path)
	}

	if wantsType(b.Type) {
		if b.Font == "" {
			b.Font = defaultFontID
		}
		if b.Size == 0 {
			if tf, ok := lookupFace(b.Font); ok {
				min, _ := tf.SizeRange()
				b.Size = min
			}
		}
	}

	for i := range b.Rows {
		b.Rows[i].Label = clean(b.Rows[i].Label, maxLabelLen)
		b.Rows[i].Value = clean(b.Rows[i].Value, maxValueLen)
	}
	for i := range b.Items {
		b.Items[i] = clean(b.Items[i], maxValueLen)
	}
	return b
}

// wantsType reports whether this block type draws text and therefore needs a
// font and size.
func wantsType(t string) bool {
	switch t {
	case BlockText, BlockClock, BlockDate, BlockRows, BlockList, BlockProgress, BlockData:
		return true
	}
	return false
}

// Validate checks one block against the screen it lives on. Errors name the
// block, its type and the exact field.
func (b Block) Validate(i int, sc Screen, imageIDs map[string]bool) error {
	if b.Type == "" {
		return berr(i, "", "type", "is missing. known types: %s", strings.Join(blockTypes, ", "))
	}
	if !contains(blockTypes, b.Type) {
		return blockError{Index: i, Msg: fmt.Sprintf("unknown block type %q. known types: %s", b.Type, strings.Join(blockTypes, ", "))}
	}
	if !contains(anchors, b.Anchor) {
		return berr(i, b.Type, "anchor", "must be one of %s, got %q", strings.Join(anchors, ", "), b.Anchor)
	}
	if !contains(aligns, b.Align) {
		return berr(i, b.Type, "align", "must be one of %s, got %q", strings.Join(aligns, ", "), b.Align)
	}

	// Geometry.
	if b.W <= 0 {
		return berr(i, b.Type, "w", "must be at least 1 pixel, got %d", b.W)
	}
	if b.H <= 0 {
		return berr(i, b.Type, "h", "must be at least 1 pixel, got %d", b.H)
	}
	if b.X < 0 {
		return berr(i, b.Type, "x", "must not be negative, got %d", b.X)
	}
	if b.Y < 0 {
		return berr(i, b.Type, "y", "must not be negative, got %d", b.Y)
	}
	if b.X+b.W > sc.W {
		return berr(i, b.Type, "x", "%d plus \"w\" %d is %d, past the right edge of a %dpx wide screen", b.X, b.W, b.X+b.W, sc.W)
	}
	if b.Y+b.H > sc.H {
		return berr(i, b.Type, "y", "%d plus \"h\" %d is %d, past the bottom of a %dpx tall screen", b.Y, b.H, b.Y+b.H, sc.H)
	}

	// Type on a 1-bit panel: the font and the size are one decision.
	if wantsType(b.Type) {
		tf, ok := lookupFace(b.Font)
		if !ok {
			return berr(i, b.Type, "font", "is not a known face: %q. available: %s", b.Font, strings.Join(faceIDs(), ", "))
		}
		if advice := tf.SizeAdvice(b.Size); advice != "" {
			return berr(i, b.Type, "size", "%s", advice)
		}
	}

	switch b.Type {
	case BlockText:
		if strings.TrimSpace(b.Text) == "" {
			return berr(i, b.Type, "text", "is required and must not be blank")
		}
	case BlockClock:
		if _, ok := findFormat(clockFormats, b.Format); !ok {
			return berr(i, b.Type, "format", "must be one of %s, got %q", formatIDs(clockFormats), b.Format)
		}
	case BlockDate:
		if _, ok := findFormat(dateFormats, b.Format); !ok {
			return berr(i, b.Type, "format", "must be one of %s, got %q", formatIDs(dateFormats), b.Format)
		}
	case BlockRows:
		if len(b.Rows) == 0 {
			return berr(i, b.Type, "rows", "is required: add at least one label/value pair")
		}
		if len(b.Rows) > maxRows {
			return berr(i, b.Type, "rows", "has %d entries, the cap is %d", len(b.Rows), maxRows)
		}
	case BlockList:
		if len(b.Items) == 0 {
			return berr(i, b.Type, "items", "is required: add at least one line")
		}
		if len(b.Items) > maxListItems {
			return berr(i, b.Type, "items", "has %d entries, the cap is %d", len(b.Items), maxListItems)
		}
	case BlockDivider:
		if !contains(orients, b.Orient) {
			return berr(i, b.Type, "orient", "must be horizontal or vertical, got %q", b.Orient)
		}
		if b.Thickness < 1 || b.Thickness > 64 {
			return berr(i, b.Type, "thickness", "must be between 1 and 64 pixels, got %d", b.Thickness)
		}
	case BlockBox:
		if !b.Fill && (b.Thickness < 1 || b.Thickness > 64) {
			return berr(i, b.Type, "thickness", "must be between 1 and 64 pixels for an outlined box, got %d", b.Thickness)
		}
	case BlockProgress:
		if b.Value < 0 || b.Value > 100 {
			return berr(i, b.Type, "value", "must be between 0 and 100, got %g", b.Value)
		}
	case BlockImage:
		if b.Image == "" {
			return berr(i, b.Type, "image", "is required: upload an image and pick it")
		}
		if imageIDs != nil && !imageIDs[b.Image] {
			return berr(i, b.Type, "image", "refers to %q, which is not an uploaded image", b.Image)
		}
		if !contains(dithers, b.Dither) {
			return berr(i, b.Type, "dither", "must be one of %s, got %q", strings.Join(dithers, ", "), b.Dither)
		}
		if !contains(fits, b.Fit) {
			return berr(i, b.Type, "fit", "must be one of %s, got %q", strings.Join(fits, ", "), b.Fit)
		}
	case BlockData:
		if b.URL == "" {
			return berr(i, b.Type, "url", "is required")
		}
		if len(b.URL) > maxURLLen {
			return berr(i, b.Type, "url", "is %d characters, the cap is %d", len(b.URL), maxURLLen)
		}
		u, err := url.Parse(b.URL)
		if err != nil {
			return berr(i, b.Type, "url", "is not a URL: %v", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return berr(i, b.Type, "url", "must be http or https, got %q", u.Scheme)
		}
		if u.Host == "" {
			return berr(i, b.Type, "url", "has no host: %q", b.URL)
		}
		if b.Path == "" {
			return berr(i, b.Type, "path", "is required: the dotted path to the value, e.g. current.temp_c")
		}
		if len(b.Path) > maxPathLen {
			return berr(i, b.Type, "path", "is %d characters, the cap is %d", len(b.Path), maxPathLen)
		}
		if b.Interval < minInterval || b.Interval > maxInterval {
			return berr(i, b.Type, "interval", "must be between %d and %d seconds, got %d", minInterval, maxInterval, b.Interval)
		}
		if b.Timeout < minTimeout || b.Timeout > maxTimeout {
			return berr(i, b.Type, "timeout", "must be between %d and %d seconds, got %d", minTimeout, maxTimeout, b.Timeout)
		}
		if !strings.Contains(b.Text, "{{v}}") {
			return berr(i, b.Type, "text", "must contain {{v}}, the placeholder the fetched value goes into")
		}
	}
	return nil
}

// NeedsSeconds reports whether this block's format renders seconds, which
// changes the whole screen's render granularity.
func (b Block) NeedsSeconds() bool {
	if b.Type != BlockClock {
		return false
	}
	f, ok := findFormat(clockFormats, b.Format)
	return ok && f.Seconds
}

func faceIDs() []string {
	out := make([]string, len(faces))
	for i, f := range faces {
		out[i] = f.ID
	}
	return out
}

func formatIDs(list []namedFormat) string {
	out := make([]string, len(list))
	for i, f := range list {
		out[i] = f.ID
	}
	return strings.Join(out, ", ")
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
