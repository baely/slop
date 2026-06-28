package export

import (
	"bufio"
	"encoding/json"
	"io"
	"time"

	"github.com/baileybutler/traccar-tools/internal/traccar"
)

// geoJSONEnc writes a FeatureCollection with one Feature per segment. Geometry
// is a LineString (or Point for a single fix) of [lon, lat, ele]; properties
// carry coordTimes plus a parallel "points" array with the full record for
// each fix, so nothing is lost.
type geoJSONEnc struct {
	bw        *bufio.Writer
	err       error
	needComma bool

	device string
	seg    int
	pts    []traccar.Position
}

func newGeoJSON(w io.Writer) *geoJSONEnc { return &geoJSONEnc{bw: bufio.NewWriter(w)} }

func (e *geoJSONEnc) ContentType() string { return "application/geo+json" }
func (e *geoJSONEnc) Ext() string         { return "geojson" }
func (e *geoJSONEnc) Err() error          { return e.err }
func (e *geoJSONEnc) Flush() error {
	if e.err != nil {
		return e.err
	}
	return e.bw.Flush()
}

func (e *geoJSONEnc) s(s string) {
	if e.err == nil {
		_, e.err = e.bw.WriteString(s)
	}
}
func (e *geoJSONEnc) raw(b []byte) {
	if e.err == nil {
		_, e.err = e.bw.Write(b)
	}
}

func (e *geoJSONEnc) Header(name string) {
	e.s(`{"type":"FeatureCollection","name":`)
	b, _ := json.Marshal(name)
	e.raw(b)
	e.s(`,"features":[`)
}
func (e *geoJSONEnc) Footer() { e.s("]}\n") }

func (e *geoJSONEnc) BeginTrack(name string) { e.device = name; e.seg = 0 }
func (e *geoJSONEnc) EndTrack()              {}

func (e *geoJSONEnc) BeginSeg()                { e.pts = e.pts[:0] }
func (e *geoJSONEnc) Point(p traccar.Position) { e.pts = append(e.pts, p) }

func (e *geoJSONEnc) EndSeg() {
	if len(e.pts) == 0 {
		return
	}
	e.seg++

	coords := make([][]float64, len(e.pts))
	times := make([]string, len(e.pts))
	points := make([]map[string]any, len(e.pts))
	for i, p := range e.pts {
		c := []float64{p.Longitude, p.Latitude}
		if p.Altitude != 0 {
			c = append(c, p.Altitude)
		}
		coords[i] = c
		if t := p.Time(); !t.IsZero() {
			times[i] = t.UTC().Format(time.RFC3339)
		}
		points[i] = pointProps(p)
	}

	var geometry map[string]any
	if len(coords) == 1 {
		geometry = map[string]any{"type": "Point", "coordinates": coords[0]}
	} else {
		geometry = map[string]any{"type": "LineString", "coordinates": coords}
	}

	feature := map[string]any{
		"type":     "Feature",
		"geometry": geometry,
		"properties": map[string]any{
			"device":     e.device,
			"segment":    e.seg,
			"coordTimes": times,
			"points":     points,
		},
	}
	b, err := json.Marshal(feature)
	if err != nil {
		if e.err == nil {
			e.err = err
		}
		return
	}
	if e.needComma {
		e.s(",")
	}
	e.raw(b)
	e.needComma = true
}

// pointProps builds the full per-fix property object.
func pointProps(p traccar.Position) map[string]any {
	m := map[string]any{
		"speedKnots": p.Speed,
		"speedMs":    p.Speed * knotsToMS,
		"course":     p.Course,
		"valid":      p.Valid,
	}
	if t := p.Time(); !t.IsZero() {
		m["time"] = t.UTC().Format(time.RFC3339)
	}
	if p.Accuracy != 0 {
		m["accuracy"] = p.Accuracy
	}
	if p.Altitude != 0 {
		m["altitude"] = p.Altitude
	}
	if p.Protocol != "" {
		m["protocol"] = p.Protocol
	}
	if p.Address != "" {
		m["address"] = p.Address
	}
	if !p.DeviceTime.IsZero() {
		m["deviceTime"] = p.DeviceTime.UTC().Format(time.RFC3339)
	}
	if !p.ServerTime.IsZero() {
		m["serverTime"] = p.ServerTime.UTC().Format(time.RFC3339)
	}
	if len(p.Attributes) > 0 {
		m["attributes"] = p.Attributes
	}
	return m
}
