// Package gpx parses GPX 1.1 into a track.Track. GPX is plain XML and
// encoding/xml is sufficient; recorder-specific extensions are ignored on
// purpose so that no recorder quirk leaks into the data model.
package gpx

import (
	"encoding/xml"
	"fmt"
	"io"
	"time"

	"github.com/giobuilds/sturdy-goggles/internal/track"
)

type file struct {
	Tracks []struct {
		Segments []struct {
			Points []point `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
}

type point struct {
	Lat  float64  `xml:"lat,attr"`
	Lon  float64  `xml:"lon,attr"`
	Ele  *float64 `xml:"ele"`
	Time string   `xml:"time"`
}

func Parse(r io.Reader) (track.Track, error) {
	var f file
	if err := xml.NewDecoder(r).Decode(&f); err != nil {
		return track.Track{}, fmt.Errorf("gpx: %w", err)
	}
	var t track.Track
	for _, trk := range f.Tracks {
		for _, seg := range trk.Segments {
			for _, p := range seg.Points {
				pt := track.Point{Lat: p.Lat, Lon: p.Lon}
				if p.Ele != nil {
					pt.Ele, pt.HasEle = *p.Ele, true
				}
				if p.Time != "" {
					ts, err := time.Parse(time.RFC3339Nano, p.Time)
					if err != nil {
						return track.Track{}, fmt.Errorf("gpx: bad time %q: %w", p.Time, err)
					}
					pt.Time = ts.UTC()
				}
				t.Points = append(t.Points, pt)
			}
		}
	}
	if len(t.Points) == 0 {
		return t, fmt.Errorf("gpx: no track points")
	}
	return t, nil
}
