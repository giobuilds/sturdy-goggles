// Package track holds the source-independent representation of a recorded
// route (walk or ride) and derives metrics from it. Both the GPX and FIT
// parsers produce a Track; everything numeric downstream comes from here, so
// that a metric definition changes in exactly one place and `rederive` can
// recompute history.
package track

import (
	"math"
	"time"
)

// DeriveVersion is stored with every derived row. Bump it whenever Derive's
// output could change for the same input, then run `fitlog rederive`.
const DeriveVersion = 1

type Point struct {
	Lat, Lon float64
	Ele      float64
	HasEle   bool
	Time     time.Time
	HR       int // beats per minute; 0 means no reading
}

type Track struct {
	Points []Point
	// Mode is the best guess from the source: "walk", "ride", "run", or ""
	// when the source carries no sport (GPX).
	Mode string
}

// Params are the tunable thresholds. They live in config, not as literals.
type Params struct {
	// A segment counts as moving when its average speed is at least this.
	MovingSpeedMPS float64
	// Elevation changes smaller than this are treated as noise (hysteresis).
	AscentThresholdM float64
}

type Metrics struct {
	Points    int
	DistanceM float64
	ElapsedS  int
	MovingS   int
	AscentM   float64
	StartLat  float64
	StartLon  float64
	EndLat    float64
	EndLon    float64
	AvgHR     int // 0 when the track has no heart-rate data
	MaxHR     int
}

// Derive computes metrics for a track. It never fails: a track with fewer
// than two points yields zeros apart from the point count and start/end.
func Derive(t Track, p Params) Metrics {
	m := Metrics{Points: len(t.Points)}
	if len(t.Points) == 0 {
		return m
	}
	first, last := t.Points[0], t.Points[len(t.Points)-1]
	m.StartLat, m.StartLon = first.Lat, first.Lon
	m.EndLat, m.EndLon = last.Lat, last.Lon
	if !first.Time.IsZero() && !last.Time.IsZero() {
		m.ElapsedS = int(last.Time.Sub(first.Time).Seconds())
	}

	var moving float64
	for i := 1; i < len(t.Points); i++ {
		a, b := t.Points[i-1], t.Points[i]
		d := Haversine(a.Lat, a.Lon, b.Lat, b.Lon)
		m.DistanceM += d
		if a.Time.IsZero() || b.Time.IsZero() {
			continue
		}
		dt := b.Time.Sub(a.Time).Seconds()
		if dt > 0 && d/dt >= p.MovingSpeedMPS {
			moving += dt
		}
	}
	m.MovingS = int(math.Round(moving))
	m.AscentM = ascent(t.Points, p.AscentThresholdM)
	m.AvgHR, m.MaxHR = heartRate(t.Points)
	return m
}

// ascent sums climbs using symmetric hysteresis: a new base is set only when
// elevation moves more than threshold away from the current base in either
// direction, so GPS jitter below the threshold contributes nothing.
func ascent(pts []Point, threshold float64) float64 {
	var total float64
	var base float64
	haveBase := false
	for _, pt := range pts {
		if !pt.HasEle {
			continue
		}
		if !haveBase {
			base, haveBase = pt.Ele, true
			continue
		}
		switch {
		case pt.Ele-base >= threshold:
			total += pt.Ele - base
			base = pt.Ele
		case base-pt.Ele >= threshold:
			base = pt.Ele
		}
	}
	return total
}

func heartRate(pts []Point) (avg, max int) {
	var sum, n int
	for _, pt := range pts {
		if pt.HR <= 0 {
			continue
		}
		sum += pt.HR
		n++
		if pt.HR > max {
			max = pt.HR
		}
	}
	if n == 0 {
		return 0, 0
	}
	return int(math.Round(float64(sum) / float64(n))), max
}

const earthRadiusM = 6371000.0

// Haversine returns the great-circle distance in metres between two points.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	φ1, φ2 := lat1*math.Pi/180, lat2*math.Pi/180
	dφ := (lat2 - lat1) * math.Pi / 180
	dλ := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dφ/2)*math.Sin(dφ/2) + math.Cos(φ1)*math.Cos(φ2)*math.Sin(dλ/2)*math.Sin(dλ/2)
	return 2 * earthRadiusM * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
