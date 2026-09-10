package fitfile

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/giobuilds/sturdy-goggles/internal/track"
)

// The real FIT fixture lives in the gitignored fixtures/ directory because it
// carries GPS coordinates. The test runs when it is present and skips otherwise.
func fixture(t *testing.T) string {
	t.Helper()
	matches, _ := filepath.Glob("../../fixtures/*.fit")
	if len(matches) == 0 {
		t.Skip("no fixtures/*.fit present")
	}
	return matches[0]
}

func TestParseBrytonRide(t *testing.T) {
	f, err := os.Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr, dev, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Mode != "ride" {
		t.Fatalf("mode = %q (sport %s), want ride", tr.Mode, dev.Sport)
	}
	if len(tr.Points) < 100 {
		t.Fatalf("only %d points", len(tr.Points))
	}
	m := track.Derive(tr, track.Params{MovingSpeedMPS: 1.0, AscentThresholdM: 3})
	t.Logf("derived: %+v", m)
	t.Logf("device:  %+v", dev)
	if m.AvgHR == 0 || m.MaxHR == 0 {
		t.Fatal("expected heart-rate data from the strap")
	}
	if dev.DistanceM > 0 {
		if rel := math.Abs(m.DistanceM-dev.DistanceM) / dev.DistanceM; rel > 0.03 {
			t.Fatalf("distance %.0f vs device %.0f (%.1f%% off)", m.DistanceM, dev.DistanceM, rel*100)
		}
	}
	if dev.AvgHR > 0 && math.Abs(float64(m.AvgHR-dev.AvgHR)) > 3 {
		t.Fatalf("avg HR %d vs device %d", m.AvgHR, dev.AvgHR)
	}
	// The device's elapsed time spans session start to end; ours spans the
	// first to last positioned record, which starts after the GPS fix.
	if dev.ElapsedS > 0 && math.Abs(float64(m.ElapsedS)-dev.ElapsedS) > 60 {
		t.Fatalf("elapsed %d vs device %.0f", m.ElapsedS, dev.ElapsedS)
	}
}
