package seed

import (
	"testing"

	"github.com/giobuilds/sturdy-goggles/internal/db"
	"github.com/giobuilds/sturdy-goggles/internal/store"
)

func TestSeedLoadsTwiceWithoutDuplicates(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := store.New(d)
	n1, err := Load(s)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := Load(s)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListExercises("")
	if err != nil {
		t.Fatal(err)
	}
	if n1 != n2 || len(list) != n1 {
		t.Fatalf("seed: first=%d second=%d rows=%d", n1, n2, len(list))
	}
	seen := map[string]bool{}
	for _, e := range list {
		if seen[e.Slug] {
			t.Fatalf("duplicate slug %s", e.Slug)
		}
		seen[e.Slug] = true
		if len(e.Tags) == 0 {
			t.Fatalf("%s has no tags", e.Slug)
		}
		if e.RegressionOf != "" && !seen[e.RegressionOf] && e.RegressionOf != e.Slug {
			// Order in ListExercises is by discipline, so only check existence.
			found := false
			for _, o := range list {
				if o.Slug == e.RegressionOf {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s: regression_of %s missing", e.Slug, e.RegressionOf)
			}
		}
	}
}

func TestStartersLoadOnceAndReferenceRealExercises(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := store.New(d)
	if _, err := Load(s); err != nil {
		t.Fatal(err)
	}
	n1, err := LoadStarters(s)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := LoadStarters(s)
	if err != nil {
		t.Fatal(err)
	}
	if n1 == 0 || n2 != 0 {
		t.Fatalf("starters: first=%d second=%d", n1, n2)
	}
	ps, _ := s.ListProtocols(false)
	for _, p := range ps {
		items, err := s.WorkoutItems(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 || !p.Starter || p.Rounds < 1 {
			t.Fatalf("%s: items=%d starter=%v rounds=%d", p.Name, len(items), p.Starter, p.Rounds)
		}
		for _, it := range items {
			if it.ExerciseID == nil || it.Target() == 0 {
				t.Fatalf("%s item %d: %+v", p.Name, it.Position, it)
			}
		}
	}
}
