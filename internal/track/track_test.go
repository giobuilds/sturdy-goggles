package track

import (
	"math"
	"testing"
	"time"
)

func TestHaversineLondonParis(t *testing.T) {
	d := Haversine(51.5074, -0.1278, 48.8566, 2.3522)
	if math.Abs(d-343_500) > 3_500 {
		t.Fatalf("London-Paris = %.0f m, want ~343500", d)
	}
}

func TestDeriveMovingAndDistance(t *testing.T) {
	t0 := time.Date(2026, 9, 9, 20, 0, 0, 0, time.UTC)
	// ~111 m per 0.001 degree of latitude.
	tr := Track{Points: []Point{
		{Lat: 50.000, Lon: 1, Time: t0},
		{Lat: 50.001, Lon: 1, Time: t0.Add(60 * time.Second)},  // 111 m / 60 s: moving
		{Lat: 50.001, Lon: 1, Time: t0.Add(180 * time.Second)}, // 0 m / 120 s: paused
		{Lat: 50.002, Lon: 1, Time: t0.Add(240 * time.Second)}, // moving
	}}
	m := Derive(tr, Params{MovingSpeedMPS: 0.5, AscentThresholdM: 3})
	if m.Points != 4 || m.ElapsedS != 240 || m.MovingS != 120 {
		t.Fatalf("got %+v", m)
	}
	if math.Abs(m.DistanceM-222) > 2 {
		t.Fatalf("distance %.1f, want ~222", m.DistanceM)
	}
	if m.StartLat != 50 || m.EndLat != 50.002 {
		t.Fatalf("start/end wrong: %+v", m)
	}
}

func TestAscentHysteresis(t *testing.T) {
	ele := func(e float64) Point { return Point{Ele: e, HasEle: true} }
	pts := []Point{ele(10), ele(11), ele(10.5), ele(14), ele(13), ele(20), ele(10), ele(15)}
	// 10→14 (+4), 14→20 (+6), drop to 10 resets base, 10→15 (+5) = 15. Jitter <3 ignored.
	if got := ascent(pts, 3); got != 15 {
		t.Fatalf("ascent = %v, want 15", got)
	}
}

func TestHeartRateIgnoresZero(t *testing.T) {
	avg, max := heartRate([]Point{{HR: 0}, {HR: 120}, {HR: 140}, {HR: 0}})
	if avg != 130 || max != 140 {
		t.Fatalf("avg=%d max=%d", avg, max)
	}
}
