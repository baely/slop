// Package export streams Traccar position history into a chosen file format
// (GPX, KML or GeoJSON), preserving the full record: timestamps, elevation,
// speed, course, accuracy and every device attribute.
package export

import (
	"fmt"
	"io"
	"strconv"

	"github.com/baileybutler/traccar-tools/internal/traccar"
)

const knotsToMS = 0.5144444444

// Encoder streams one document. The caller drives it as:
//
//	Header(name)
//	for each device: BeginTrack(name); for each segment: BeginSeg(); Point(p)…; EndSeg(); EndTrack()
//	Footer(); Flush()
type Encoder interface {
	Header(name string)
	BeginTrack(name string)
	BeginSeg()
	Point(p traccar.Position)
	EndSeg()
	EndTrack()
	Footer()
	Err() error
	Flush() error
	ContentType() string
	Ext() string
}

// New builds an encoder for the given format ("gpx", "kml", "geojson").
func New(format string, w io.Writer) (Encoder, error) {
	switch format {
	case "", "gpx":
		return newGPX(w), nil
	case "kml":
		return newKML(w), nil
	case "geojson", "json":
		return newGeoJSON(w), nil
	default:
		return nil, fmt.Errorf("unknown export format %q", format)
	}
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// scalar renders an attribute value as a plain string for XML formats.
func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
