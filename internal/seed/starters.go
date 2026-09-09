package seed

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/giobuilds/sturdy-goggles/internal/store"
)

//go:embed starters.json
var startersJSON []byte

type starter struct {
	Name   string `json:"name"`
	Notes  string `json:"notes"`
	Rounds int    `json:"rounds"`
	Items  []struct {
		Exercise string   `json:"exercise"`
		Label    string   `json:"label"`
		Reps     *int     `json:"reps"`
		Secs     *int     `json:"secs"`
		Rest     int      `json:"rest"`
		Load     *float64 `json:"load"`
	} `json:"items"`
}

// LoadStarters creates the shipped beginner workouts as protocols. A starter
// whose name already exists is left alone, so user edits survive upgrades.
// Returns the number created.
func LoadStarters(s *store.Store) (int, error) {
	var in []starter
	if err := json.Unmarshal(startersJSON, &in); err != nil {
		return 0, fmt.Errorf("starters: %w", err)
	}
	created := 0
	for _, st := range in {
		if _, err := s.ProtocolByName(st.Name); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return created, err
		}
		id, err := s.CreateProtocol(store.Protocol{Kind: "workout", Name: st.Name, Notes: st.Notes, Rounds: st.Rounds, Starter: true})
		if err != nil {
			return created, err
		}
		items := make([]store.WorkoutItem, 0, len(st.Items))
		for _, it := range st.Items {
			ex, err := s.ExerciseBySlug(it.Exercise)
			if err != nil {
				return created, fmt.Errorf("starter %q: exercise %q: %w", st.Name, it.Exercise, err)
			}
			label := it.Label
			if label == "" {
				label = ex.Name
			}
			items = append(items, store.WorkoutItem{
				ExerciseID: &ex.ID, Label: label, TargetReps: it.Reps, TargetSecs: it.Secs, RestSecs: it.Rest, LoadKg: it.Load,
			})
		}
		if err := s.ReplaceWorkoutItems(id, items); err != nil {
			return created, fmt.Errorf("starter %q: %w", st.Name, err)
		}
		created++
	}
	return created, nil
}
