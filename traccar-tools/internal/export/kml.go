package export

import (
	"bufio"
	"encoding/xml"
	"io"
	"strconv"
	"time"

	"github.com/baileybutler/traccar-tools/internal/traccar"
)

// kmlEnc writes standard KML 2.2 (no Google gx: extensions, which some parsers
// reject). Each segment becomes a <LineString> Placemark for the path plus one
// <Placemark> per fix carrying a <TimeStamp> and an <ExtendedData> block, so
// timestamps, speed, course, accuracy and every attribute are preserved and the
// file imports anywhere.
type kmlEnc struct {
	bw  *bufio.Writer
	err error

	device string
	seg    int
	pts    []traccar.Position
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
	k.s(`<kml xmlns="http://www.opengis.net/kml/2.2">` + "\n")
	k.s("  <Document>\n    <name>")
	k.esc(name)
	k.s("</name>\n")
}
func (k *kmlEnc) Footer() { k.s("  </Document>\n</kml>\n") }

func (k *kmlEnc) BeginTrack(name string) {
	k.device = name
	k.seg = 0
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
	k.seg++
	label := k.device + " — segment " + strconv.Itoa(k.seg)

	// Path as a single LineString (only when there is more than one fix).
	if len(k.pts) > 1 {
		k.s("      <Placemark>\n        <name>")
		k.esc(label)
		k.s("</name>\n        <LineString>\n          <altitudeMode>absolute</altitudeMode>\n          <coordinates>\n")
		for _, p := range k.pts {
			k.s("            ")
			k.s(num(p.Longitude))
			k.s(",")
			k.s(num(p.Latitude))
			k.s(",")
			k.s(num(p.Altitude))
			k.s("\n")
		}
		k.s("          </coordinates>\n        </LineString>\n      </Placemark>\n")
	}

	// Each fix as a Point Placemark with time + full data.
	for _, p := range k.pts {
		k.s("      <Placemark>\n")
		if t := p.Time(); !t.IsZero() {
			k.s("        <TimeStamp><when>")
			k.s(t.UTC().Format(time.RFC3339))
			k.s("</when></TimeStamp>\n")
		}
		k.s("        <ExtendedData>\n")
		k.data("speedKnots", num(p.Speed))
		k.data("speedMs", num(p.Speed*knotsToMS))
		k.data("course", num(p.Course))
		if p.Accuracy != 0 {
			k.data("accuracy", num(p.Accuracy))
		}
		k.data("valid", strconv.FormatBool(p.Valid))
		if p.Protocol != "" {
			k.data("protocol", p.Protocol)
		}
		if p.Address != "" {
			k.data("address", p.Address)
		}
		if !p.DeviceTime.IsZero() {
			k.data("deviceTime", p.DeviceTime.UTC().Format(time.RFC3339))
		}
		if !p.ServerTime.IsZero() {
			k.data("serverTime", p.ServerTime.UTC().Format(time.RFC3339))
		}
		for _, key := range sortedKeys(p.Attributes) {
			k.data(key, scalar(p.Attributes[key]))
		}
		k.s("        </ExtendedData>\n")
		k.s("        <Point>\n          <altitudeMode>absolute</altitudeMode>\n          <coordinates>")
		k.s(num(p.Longitude))
		k.s(",")
		k.s(num(p.Latitude))
		k.s(",")
		k.s(num(p.Altitude))
		k.s("</coordinates>\n        </Point>\n      </Placemark>\n")
	}
}

func (k *kmlEnc) data(name, val string) {
	k.s(`          <Data name="`)
	k.esc(name)
	k.s("\"><value>")
	k.esc(val)
	k.s("</value></Data>\n")
}
