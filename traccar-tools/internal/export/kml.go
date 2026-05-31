package export

import (
	"bufio"
	"encoding/xml"
	"io"
	"time"

	"github.com/baileybutler/traccar-tools/internal/traccar"
)

// kmlEnc writes KML using gx:Track, which pairs <when> timestamps with
// <gx:coord> so the track is time-aware (and animatable in Google Earth).
// Per-point values are buffered per segment and emitted as gx:SimpleArrayData,
// taking the union of attribute keys so every array has equal length.
type kmlEnc struct {
	bw  *bufio.Writer
	err error

	pts []traccar.Position
}

func newKML(w io.Writer) *kmlEnc { return &kmlEnc{bw: bufio.NewWriter(w)} }

func (k *kmlEnc) ContentType() string { return "application/vnd.google-earth.kml+xml" }
func (k *kmlEnc) Ext() string         { return "kml" }
func (k *kmlEnc) Err() error          { return k.err }
func (k *kmlEnc) Flush() error {
	if k.err != nil {
		return k.err
	}
	return k.bw.Flush()
}

func (k *kmlEnc) s(s string) {
	if k.err == nil {
		_, k.err = k.bw.WriteString(s)
	}
}
func (k *kmlEnc) esc(s string) {
	if k.err == nil {
		k.err = xml.EscapeText(k.bw, []byte(s))
	}
}

func (k *kmlEnc) Header(name string) {
	k.s(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	k.s(`<kml xmlns="http://www.opengis.net/kml/2.2" xmlns:gx="http://www.google.com/kml/ext/2.2">` + "\n")
	k.s("  <Document>\n    <name>")
	k.esc(name)
	k.s("</name>\n")
}
func (k *kmlEnc) Footer() { k.s("  </Document>\n</kml>\n") }

func (k *kmlEnc) BeginTrack(name string) {
	k.s("    <Folder>\n      <name>")
	k.esc(name)
	k.s("</name>\n")
}
func (k *kmlEnc) EndTrack() { k.s("    </Folder>\n") }

func (k *kmlEnc) BeginSeg()                { k.pts = k.pts[:0] }
func (k *kmlEnc) Point(p traccar.Position) { k.pts = append(k.pts, p) }

func (k *kmlEnc) EndSeg() {
	if len(k.pts) == 0 {
		return
	}
	k.s("      <Placemark>\n        <gx:Track>\n          <altitudeMode>absolute</altitudeMode>\n")
	for _, p := range k.pts {
		k.s("          <when>")
		if t := p.Time(); !t.IsZero() {
			k.s(t.UTC().Format(time.RFC3339))
		}
		k.s("</when>\n")
	}
	for _, p := range k.pts {
		k.s("          <gx:coord>")
		k.s(num(p.Longitude))
		k.s(" ")
		k.s(num(p.Latitude))
		k.s(" ")
		k.s(num(p.Altitude))
		k.s("</gx:coord>\n")
	}

	// Union of attribute keys across the segment, for equal-length arrays.
	keySet := map[string]bool{}
	for _, p := range k.pts {
		for key := range p.Attributes {
			keySet[key] = true
		}
	}
	k.s("          <ExtendedData>\n            <SchemaData schemaUrl=\"#traccar\">\n")
	k.array("speedKnots", func(p traccar.Position) string { return num(p.Speed) })
	k.array("speedMs", func(p traccar.Position) string { return num(p.Speed * knotsToMS) })
	k.array("course", func(p traccar.Position) string { return num(p.Course) })
	k.array("accuracy", func(p traccar.Position) string { return num(p.Accuracy) })
	for _, key := range sortedKeys(toAnyMap(keySet)) {
		key := key
		k.array(key, func(p traccar.Position) string {
			if v, ok := p.Attributes[key]; ok {
				return scalar(v)
			}
			return ""
		})
	}
	k.s("            </SchemaData>\n          </ExtendedData>\n")
	k.s("        </gx:Track>\n      </Placemark>\n")
}

func (k *kmlEnc) array(name string, val func(traccar.Position) string) {
	k.s(`              <gx:SimpleArrayData name="`)
	k.esc(name)
	k.s("\">\n")
	for _, p := range k.pts {
		k.s("                <gx:value>")
		k.esc(val(p))
		k.s("</gx:value>\n")
	}
	k.s("              </gx:SimpleArrayData>\n")
}

func toAnyMap(set map[string]bool) map[string]any {
	m := make(map[string]any, len(set))
	for k := range set {
		m[k] = nil
	}
	return m
}
