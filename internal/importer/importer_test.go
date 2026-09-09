package importer

import (
	"path/filepath"
	"testing"

	"github.com/giobuilds/sturdy-goggles/internal/db"
	"github.com/giobuilds/sturdy-goggles/internal/store"
	"github.com/giobuilds/sturdy-goggles/internal/track"
)

func newImporter(t *testing.T) *Importer {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Importer{
		Store:   store.New(d),
		DataDir: t.TempDir(),
		Params:  track.Params{MovingSpeedMPS: 0.5, AscentThresholdM: 3},
	}
}

func TestImportIsIdempotentAndRederivePreservesRating(t *testing.T) {
	im := newImporter(t)
	src := filepath.Join("..", "..", "testdata", "short-walk.gpx")

	r1, err := im.ImportFile(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Duplicate || r1.Mode != "walk" || r1.SourceKind != "gpx" || r1.Metrics.Points != 5 {
		t.Fatalf("first import: %+v", r1)
	}
	// Bytes copied under the data dir, named by hash.
	if _, err := filepathStat(im.SourcePath("gpx", r1.Hash)); err != nil {
		t.Fatal(err)
	}

	r2, err := im.ImportFile(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Duplicate || r2.SessionID != r1.SessionID {
		t.Fatalf("second import should be a duplicate of %d: %+v", r1.SessionID, r2)
	}
	sessions, _ := im.Store.ListSessions(store.SessionFilter{})
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	if err := im.Store.RateSession(r1.SessionID, 3, "felt easy"); err != nil {
		t.Fatal(err)
	}

	// Change a threshold, rederive, and check metrics moved but rating did not.
	im.Params.MovingSpeedMPS = 1000 // nothing is "moving" now
	n, err := im.Rederive()
	if err != nil || n != 1 {
		t.Fatalf("rederive n=%d err=%v", n, err)
	}
	m, err := im.Store.RouteMetricsBySession(r1.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if m.MovingS != 0 || m.ElapsedS != 47 {
		t.Fatalf("after rederive: %+v", m.Metrics)
	}
	sess, _ := im.Store.SessionByID(r1.SessionID)
	if sess.Effort == nil || *sess.Effort != 3 || sess.Note != "felt easy" {
		t.Fatalf("rating lost: %+v", sess)
	}
}

func TestImportRejectsProtocolModeMismatch(t *testing.T) {
	im := newImporter(t)
	pid, err := im.Store.CreateProtocol(store.Protocol{Kind: "route", Mode: "ride", Name: "Canal loop"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = im.ImportFile(filepath.Join("..", "..", "testdata", "short-walk.gpx"), Options{Mode: "walk", ProtocolID: &pid})
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestImportRealFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "fixtures", "*"))
	if len(files) == 0 {
		t.Skip("no private fixtures present")
	}
	im := newImporter(t)
	for _, f := range files {
		r, err := im.ImportFile(f, Options{})
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		t.Logf("%s -> session %d mode=%s %+v", filepath.Base(f), r.SessionID, r.Mode, r.Metrics)
		if r.Device != nil {
			t.Logf("   device totals: %+v", *r.Device)
		}
	}
}
