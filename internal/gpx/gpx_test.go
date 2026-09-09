package gpx

import (
	"os"
	"testing"

	"github.com/giobuilds/sturdy-goggles/internal/track"
)

func TestParseShortWalk(t *testing.T) {
	f, err := os.Open("../../testdata/short-walk.gpx")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Points) != 5 {
		t.Fatalf("points = %d, want 5", len(tr.Points))
	}
	if !tr.Points[0].HasEle {
		t.Fatal("expected elevation")
	}
	m := track.Derive(tr, track.Params{MovingSpeedMPS: 0.5, AscentThresholdM: 3})
	if m.ElapsedS != 47 {
		t.Fatalf("elapsed = %d, want 47", m.ElapsedS)
	}
	if m.DistanceM <= 0 || m.DistanceM > 50 {
		t.Fatalf("distance = %.1f, want a few metres", m.DistanceM)
	}
	if m.AvgHR != 0 {
		t.Fatalf("GPX has no HR, got %d", m.AvgHR)
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse(stringsReader(`<gpx><trk><trkseg></trkseg></trk></gpx>`)); err == nil {
		t.Fatal("expected error for empty track")
	}
}
