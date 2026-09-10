// Package fitfile parses Garmin FIT activity files (as written by the Bryton
// Rider 420 and most head units) into a track.Track. It keeps the device's own
// session totals alongside, so tests can check our derivation against them.
package fitfile

import (
	"fmt"
	"io"
	"math"

	"github.com/tormoder/fit"

	"github.com/giobuilds/sturdy-goggles/internal/track"
)

// DeviceTotals are what the head unit itself computed. Stored nowhere; used
// to sanity-check Derive and shown at import time.
type DeviceTotals struct {
	DistanceM float64
	TimerS    float64
	ElapsedS  float64
	AscentM   float64
	AvgHR     int
	MaxHR     int
	Sport     string
}

func Parse(r io.Reader) (track.Track, DeviceTotals, error) {
	f, err := fit.Decode(r)
	if err != nil {
		return track.Track{}, DeviceTotals{}, fmt.Errorf("fit: %w", err)
	}
	act, err := f.Activity()
	if err != nil {
		return track.Track{}, DeviceTotals{}, fmt.Errorf("fit: %w", err)
	}

	var t track.Track
	for _, rec := range act.Records {
		if rec.PositionLat.Invalid() || rec.PositionLong.Invalid() {
			continue
		}
		pt := track.Point{
			Lat:  rec.PositionLat.Degrees(),
			Lon:  rec.PositionLong.Degrees(),
			Time: rec.Timestamp.UTC(),
		}
		if ele := altitude(rec); !math.IsNaN(ele) {
			pt.Ele, pt.HasEle = ele, true
		}
		if rec.HeartRate != 0xFF && rec.HeartRate > 0 {
			pt.HR = int(rec.HeartRate)
		}
		t.Points = append(t.Points, pt)
	}
	if len(t.Points) == 0 {
		return t, DeviceTotals{}, fmt.Errorf("fit: no positioned records")
	}

	var totals DeviceTotals
	if len(act.Sessions) > 0 {
		s := act.Sessions[0]
		totals = DeviceTotals{
			DistanceM: nanToZero(s.GetTotalDistanceScaled()),
			TimerS:    nanToZero(s.GetTotalTimerTimeScaled()),
			ElapsedS:  nanToZero(s.GetTotalElapsedTimeScaled()),
			Sport:     s.Sport.String(),
		}
		if s.TotalAscent != 0xFFFF {
			totals.AscentM = float64(s.TotalAscent)
		}
		if s.AvgHeartRate != 0xFF {
			totals.AvgHR = int(s.AvgHeartRate)
		}
		if s.MaxHeartRate != 0xFF {
			totals.MaxHR = int(s.MaxHeartRate)
		}
		t.Mode = mode(s.Sport)
	}
	return t, totals, nil
}

func altitude(rec *fit.RecordMsg) float64 {
	if rec.EnhancedAltitude != 0xFFFFFFFF {
		return float64(rec.EnhancedAltitude)/5 - 500
	}
	return rec.GetAltitudeScaled()
}

func mode(s fit.Sport) string {
	switch s {
	case fit.SportCycling:
		return "ride"
	case fit.SportWalking, fit.SportHiking:
		return "walk"
	case fit.SportRunning:
		return "run"
	default:
		return ""
	}
}

func nanToZero(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	return f
}
