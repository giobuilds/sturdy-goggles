package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/giobuilds/sturdy-goggles/internal/db"
	"github.com/giobuilds/sturdy-goggles/internal/seed"
	"github.com/giobuilds/sturdy-goggles/internal/store"
)

func newServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	st := store.New(d)
	if _, err := seed.Load(st); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.LoadStarters(st); err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return srv, st
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestPagesRender(t *testing.T) {
	srv, st := newServer(t)
	h := srv.Handler()
	p, err := st.ProtocolByName("First session")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/start", "/sessions", "/play/1", "/protocol/1", "/manifest.webmanifest", "/sw.js", "/static/app.css", "/static/player.js"} {
		if rec := get(t, h, path); rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	home := get(t, h, "/").Body.String()
	if !strings.Contains(home, "Start a session") || !strings.Contains(home, p.Name) {
		t.Fatalf("home missing content:\n%s", home)
	}
	play := get(t, h, "/play/"+itoa(p.ID)).Body.String()
	if !strings.Contains(play, `"protocol_id":`+itoa(p.ID)) || !strings.Contains(play, "Begin") {
		t.Fatalf("play page missing plan JSON:\n%s", play)
	}
	if rec := get(t, h, "/session/999"); rec.Code != 404 {
		t.Fatalf("missing session: %d", rec.Code)
	}
}

func TestCreateRateAndProtocolStats(t *testing.T) {
	srv, st := newServer(t)
	h := srv.Handler()
	p, _ := st.ProtocolByName("First session")

	create := func(total int, scaled bool) int64 {
		body := `{"protocol_id":` + itoa(p.ID) + `,"started_at":"2026-09-10T18:00:00Z","total_secs":` + itoa(int64(total)) +
			`,"completed":true,"scaled":` + boolStr(scaled) + `,"items":[{"round":1,"position":1,"label":"Bodyweight squat","unit":"reps","target":8,"actual":8},{"round":1,"position":5,"label":"Plank","unit":"secs","target":20,"actual":20}]}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/sessions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			ID      int64  `json:"id"`
			RateURL string `json:"rate_url"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.RateURL != "/session/"+itoa(out.ID)+"/rate" {
			t.Fatalf("rate url %q", out.RateURL)
		}
		return out.ID
	}
	id1 := create(600, false)
	id2 := create(540, false)
	create(400, true) // scaled: must not count toward median/best

	// Session exercises stored with the exercise resolved from the prescription.
	items, err := st.SessionExercises(id1)
	if err != nil || len(items) != 2 || items[0].ExerciseID == nil {
		t.Fatalf("session exercises: %v %+v", err, items)
	}

	// Rate via the form.
	rec := httptest.NewRecorder()
	form := url.Values{"effort": {"3"}, "note": {"felt fine"}}
	req := httptest.NewRequest("POST", "/session/"+itoa(id1)+"/rate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rate: %d %s", rec.Code, rec.Body.String())
	}
	s1, _ := st.SessionByID(id1)
	if s1.Effort == nil || *s1.Effort != 3 || s1.Note != "felt fine" {
		t.Fatalf("rating not stored: %+v", s1)
	}

	// Bad effort rejected.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/session/"+itoa(id2)+"/rate", strings.NewReader("effort=9"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("effort=9 accepted: %d", rec.Code)
	}

	// Protocol page: 2 comparable, median 10:00 (upper median of {540,600}), best 9:00.
	page := get(t, h, "/protocol/"+itoa(p.ID)).Body.String()
	if !strings.Contains(page, "2 comparable sessions") || !strings.Contains(page, "median 10:00") || !strings.Contains(page, "best 9:00") {
		t.Fatalf("protocol stats wrong:\n%s", page)
	}
	if !strings.Contains(page, "scaled") {
		t.Fatal("scaled session not marked")
	}

	// Home shows unrated and this-week count (sessions dated 2026-09-10; test clock is now, so only check render).
	if rec := get(t, h, "/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Not yet rated") {
		t.Fatalf("home unrated section missing")
	}
}

func TestWeeklyCounts(t *testing.T) {
	_, st := newServer(t)
	loc := time.UTC
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, loc) // a Thursday
	mk := func(month time.Month, day int) {
		if _, err := st.CreateSession(store.Session{Kind: "workout", StartedAt: time.Date(2026, month, day, 19, 0, 0, 0, loc), Completed: true}); err != nil {
			t.Fatal(err)
		}
	}
	mk(9, 7)  // Mon this week (7 Sep 2026 is a Monday)
	mk(9, 9)  // Wed this week
	mk(9, 6)  // Sun last week
	mk(8, 25) // Tue, two weeks ago
	weeks, err := st.WeeklyCounts(loc, 8, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 8 || !weeks[7].Start.Equal(time.Date(2026, 9, 7, 0, 0, 0, 0, loc)) {
		t.Fatalf("weeks: %+v", weeks)
	}
	if weeks[7].Total != 2 || weeks[6].Total != 1 || weeks[5].Total != 1 {
		t.Fatalf("counts: %d %d %d", weeks[5].Total, weeks[6].Total, weeks[7].Total)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
