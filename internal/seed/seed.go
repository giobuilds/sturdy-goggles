// Package seed embeds the hand-authored exercise library and loads it into
// the store. Loading is idempotent: rows are upserted by slug.
package seed

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/giobuilds/sturdy-goggles/internal/store"
)

//go:embed exercises.json
var exercisesJSON []byte

type exercise struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Discipline   string   `json:"discipline"`
	Difficulty   int      `json:"difficulty"`
	Equipment    string   `json:"equipment"`
	Unit         string   `json:"unit"`
	RegressionOf string   `json:"regression_of"`
	Tags         []string `json:"tags"`
	Notes        string   `json:"notes"`
}

// Exercises returns the embedded library in file order (regressions first).
func Exercises() ([]store.Exercise, error) {
	var in []exercise
	if err := json.Unmarshal(exercisesJSON, &in); err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}
	out := make([]store.Exercise, 0, len(in))
	for _, e := range in {
		eq := e.Equipment
		if eq == "" {
			eq = "none"
		}
		out = append(out, store.Exercise{
			Slug: e.Slug, Name: e.Name, Discipline: e.Discipline, Difficulty: e.Difficulty,
			Equipment: eq, Unit: e.Unit, RegressionOf: e.RegressionOf, Tags: e.Tags, Notes: e.Notes,
			Source: "fitlog seed (CC0)",
		})
	}
	return out, nil
}

// Load upserts the library. Returns the number of exercises written.
func Load(s *store.Store) (int, error) {
	ex, err := Exercises()
	if err != nil {
		return 0, err
	}
	for _, e := range ex {
		if _, err := s.UpsertExercise(e); err != nil {
			return 0, err
		}
	}
	return len(ex), nil
}
