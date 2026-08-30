package export

import (
	"bufio"
	"encoding/xml"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/baileybutler/traccar-tools/internal/traccar"
)

// gpxEnc writes GPX 1.1. Standard <ele>/<time> plus a traccar: extension
// namespace carrying speed, course, accuracy and every attribute.
type gpxEnc struct {
	bw  *bufio.Writer
	err error
}

func newGPX(w io.Writer) *gpxEnc { return &gpxEnc{bw: bufio.NewWriter(w)} }

func (g *gpxEnc) ContentType() string { return "application/gpx+xml" }
func (g *gpxEnc) Ext() string         { return "gpx" }
func (g *gpxEnc) Err() error          { return g.err }
func (g *gpxEnc) Flush() error {
	if g.err != nil {
		return g.err
	}
	return g.bw.Flush()
}

func (g *gpxEnc) s(s string) {
	if g.err == nil {
		_, g.err = g.bw.WriteString(s)
	}
}
func (g *gpxEnc) esc(s string) {
	if g.err == nil {
		g.err = xml.EscapeText(g.bw, []byte(s))
	}
}

func (g *gpxEnc) Header(name string) {
	g.s(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	g.s(`<gpx version="1.1" creator="traccar-tools"` + "\n")
	g.s(`  xmlns="http://www.topografix.com/GPX/1/1"` + "\n")
	g.s(`  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"` + "\n")
	g.s(`  xmlns:traccar="https://traccar.org/gpx/extensions/v1"` + "\n")
	g.s(`  xsi:schemaLocation="http://www.topografix.com/GPX/1/1 http://www.topografix.com/GPX/1/1/gpx.xsd">` + "\n")
	g.s("  <metadata>\n    <name>")
	g.esc(name)
	g.s("</name>\n    <time>")
	g.s(time.Now().UTC().Format(time.RFC3339))
	g.s("</time>\n  </metadata>\n")
}
func (g *gpxEnc) Footer() { g.s("</gpx>\n") }

func (g *gpxEnc) BeginTrack(name string) {
	g.s("  <trk>\n    <name>")
	g.esc(name)
	g.s("</name>\n")
}
func (g *gpxEnc) EndTrack() { g.s("  </trk>\n") }
func (g *gpxEnc) BeginSeg() { g.s("    <trkseg>\n") }
func (g *gpxEnc) EndSeg()   { g.s("    </trkseg>\n") }

func (g *gpxEnc) Point(p traccar.Position) {
	g.s(`      <trkpt lat="`)
	g.s(num(p.Latitude))
	g.s(`" lon="`)
	g.s(num(p.Longitude))
	g.s("\">\n")
	if p.Altitude != 0 {
		g.s("        <ele>")
		g.s(num(p.Altitude))
		g.s("</ele>\n")
	}
	if t := p.Time(); !t.IsZero() {
		g.s("        <time>")
		g.s(t.UTC().Format(time.RFC3339))
		g.s("</time>\n")
	}
	g.s("        <extensions>\n")
	g.ext("speed", num(p.Speed*knotsToMS))
	g.ext("speedKnots", num(p.Speed))
	g.ext("course", num(p.Course))
	if p.Accuracy != 0 {
		g.ext("accuracy", num(p.Accuracy))
	}
	g.ext("valid", strconv.FormatBool(p.Valid))
	if p.Protocol != "" {
		g.ext("protocol", p.Protocol)
	}
	if p.Address != "" {
		g.ext("address", p.Address)
	}
	if !p.DeviceTime.IsZero() {
		g.ext("deviceTime", p.DeviceTime.UTC().Format(time.RFC3339))
	}
	if !p.ServerTime.IsZero() {
		g.ext("serverTime", p.ServerTime.UTC().Format(time.RFC3339))
	}
	for _, k := range sortedKeys(p.Attributes) {
		g.s(`          <traccar:attr name="`)
		g.esc(k)
		g.s(`">`)
		g.esc(scalar(p.Attributes[k]))
		g.s("</traccar:attr>\n")
	}
	g.s("        </extensions>\n")
	g.s("      </trkpt>\n")
}

func (g *gpxEnc) ext(tag, val string) {
	g.s("          <traccar:")
	g.s(tag)
	g.s(">")
	g.esc(val)
	g.s("</traccar:")
	g.s(tag)
	g.s(">\n")
}

func sortedKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
