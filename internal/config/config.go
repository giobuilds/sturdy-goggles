// Package config reads Fit Log's runtime configuration from the environment.
// Secrets are env-only by design; there is no config-file field for them.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/giobuilds/sturdy-goggles/internal/track"
)

type Config struct {
	DataDir string
	Track   track.Params
}

func Load() (Config, error) {
	c := Config{
		DataDir: envOr("FITLOG_DATA", "./data"),
		Track: track.Params{
			MovingSpeedMPS:   0.5,
			AscentThresholdM: 3,
		},
	}
	var err error
	if c.Track.MovingSpeedMPS, err = envFloat("FITLOG_MOVING_SPEED_MPS", c.Track.MovingSpeedMPS); err != nil {
		return c, err
	}
	if c.Track.AscentThresholdM, err = envFloat("FITLOG_ASCENT_THRESHOLD_M", c.Track.AscentThresholdM); err != nil {
		return c, err
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}
