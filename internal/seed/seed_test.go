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
