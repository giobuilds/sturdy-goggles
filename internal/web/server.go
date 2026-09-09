// Package web is the server-rendered PWA: home with the consistency view,
// starting and playing a workout, rating, and history. Minimal JS lives in
// static/; the session player is the one component that must work offline.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/giobuilds/sturdy-goggles/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	store *store.Store
	loc   *time.Location
	pages map[string]*template.Template
	mux   *http.ServeMux
}

func New(s *store.Store, loc *time.Location) (*Server, error) {
	if loc == nil {
		loc = time.Local
	}
	srv := &Server{store: s, loc: loc, pages: map[string]*template.Template{}, mux: http.NewServeMux()}
	funcs := template.FuncMap{
		"hms":   hms,
		"km":    func(m float64) string { return fmt.Sprintf("%.2f", m/1000) },
		"local": func(t time.Time) time.Time { return t.In(loc) },
		"day":   func(t time.Time) string { return t.In(loc).Format("Mon 2 Jan") },
		"clock": func(t time.Time) string { return t.In(loc).Format("15:04") },
		"deref": derefInt,
		"deref64": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
		"kg": func(f *float64) string {
			if f == nil {
				return ""
			}
			return strconv.FormatFloat(*f, 'f', -1, 64) + " kg"
		},
		"title": func(s string) string {
			if s == "" {
				return s
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i + 1
			}
			return out
		},
		"pct": func(n, max int) int {
			if max == 0 {
				return 0
			}
			return n * 100 / max
		},
	}
	pages := []string{"home", "start", "play", "session", "rate", "sessions", "protocol", "error"}
	for _, p := range pages {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+p+".html")
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", p, err)
		}
		srv.pages[p] = t
	}
	srv.routes()
	return srv, nil
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("GET /manifest.webmanifest", s.serveStatic("manifest.webmanifest", "application/manifest+json"))
	s.mux.HandleFunc("GET /sw.js", s.serveStatic("sw.js", "application/javascript"))

	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /start", s.handleStart)
	s.mux.HandleFunc("GET /play/{id}", s.handlePlay)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /session/{id}", s.handleSession)
	s.mux.HandleFunc("GET /session/{id}/rate", s.handleRateForm)
	s.mux.HandleFunc("POST /session/{id}/rate", s.handleRate)
	s.mux.HandleFunc("GET /sessions", s.handleSessions)
	s.mux.HandleFunc("GET /protocol/{id}", s.handleProtocol)
}

func (s *Server) serveStatic(name, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	}
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pages[page].ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

func (s *Server) fail(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	s.render(w, "error", map[string]any{"Title": "Something went wrong", "Message": msg, "Code": code})
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// ---- view models -----------------------------------------------------------

type sessionRow struct {
	store.Session
	ProtocolName string
	Result       string
	Comparable   bool
}

func (s *Server) row(sess store.Session) sessionRow {
	r := sessionRow{Session: sess, ProtocolName: "One-off", Comparable: sess.Completed && !sess.Scaled}
	if sess.ProtocolID != nil {
		if p, err := s.store.ProtocolByID(*sess.ProtocolID); err == nil {
			r.ProtocolName = p.Name
		}
	}
	switch sess.Kind {
	case "route":
		if m, err := s.store.RouteMetricsBySession(sess.ID); err == nil {
			r.Result = fmt.Sprintf("%.2f km · %s", m.DistanceM/1000, hms(m.MovingS))
			if sess.ProtocolID == nil {
				r.ProtocolName = title(sess.Mode)
			}
		}
	case "workout":
		if wr, err := s.store.WorkoutResultBySession(sess.ID); err == nil && wr.TotalSecs != nil {
			r.Result = hms(*wr.TotalSecs)
		}
	}
	return r
}

func (s *Server) rows(list []store.Session) []sessionRow {
	out := make([]sessionRow, 0, len(list))
	for _, sess := range list {
		out = append(out, s.row(sess))
	}
	return out
}

type protocolCard struct {
	store.Protocol
	Items    []store.WorkoutItem
	Minutes  int
	Sessions int
	Last     *time.Time
}

func (s *Server) card(p store.Protocol) protocolCard {
	c := protocolCard{Protocol: p}
	if p.Kind == "workout" {
		c.Items, _ = s.store.WorkoutItems(p.ID)
		c.Minutes = estimateMinutes(p.Rounds, c.Items)
	}
	sessions, _ := s.store.ListSessions(store.SessionFilter{ProtocolID: &p.ID})
	c.Sessions = len(sessions)
	if len(sessions) > 0 {
		t := sessions[0].StartedAt
		c.Last = &t
	}
	return c
}

// estimateMinutes: timed items at face value, reps at 3 s each, plus rest.
func estimateMinutes(rounds int, items []store.WorkoutItem) int {
	secs := 0
	for _, it := range items {
		if it.TargetSecs != nil {
			secs += *it.TargetSecs
		} else if it.TargetReps != nil {
			secs += *it.TargetReps * 3
		}
		secs += it.RestSecs
	}
	if rounds < 1 {
		rounds = 1
	}
	return (secs*rounds + 30) / 60
}

// ---- handlers --------------------------------------------------------------

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	weeks, err := s.store.WeeklyCounts(s.loc, 8, now)
	if err != nil {
		s.fail(w, 500, err.Error())
		return
	}
	maxWeek := 0
	for _, wk := range weeks {
		if wk.Total > maxWeek {
			maxWeek = wk.Total
		}
	}
	recent, _ := s.store.ListSessions(store.SessionFilter{Limit: 5})
	unrated, _ := s.store.UnratedSessions()
	protocols, _ := s.store.ListProtocols(false)
	var workouts []protocolCard
	for _, p := range protocols {
		if p.Kind == "workout" {
			workouts = append(workouts, s.card(p))
		}
	}
	s.render(w, "home", map[string]any{
		"Title":    "Fit Log",
		"Weeks":    weeks,
		"MaxWeek":  maxWeek,
		"ThisWeek": weeks[len(weeks)-1],
		"Recent":   s.rows(recent),
		"Unrated":  s.rows(unrated),
		"Workouts": workouts,
	})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	protocols, err := s.store.ListProtocols(false)
	if err != nil {
		s.fail(w, 500, err.Error())
		return
	}
	var cards []protocolCard
	for _, p := range protocols {
		if p.Kind == "workout" {
			cards = append(cards, s.card(p))
		}
	}
	s.render(w, "start", map[string]any{"Title": "Start a session", "Workouts": cards})
}

type planItem struct {
	Position int      `json:"position"`
	Label    string   `json:"label"`
	Unit     string   `json:"unit"`
	Target   int      `json:"target"`
	Rest     int      `json:"rest"`
	LoadKg   *float64 `json:"load_kg,omitempty"`
	Slug     string   `json:"slug,omitempty"`
}

type plan struct {
	ProtocolID int64      `json:"protocol_id"`
	Name       string     `json:"name"`
	Rounds     int        `json:"rounds"`
	Items      []planItem `json:"items"`
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, 400, "bad id")
		return
	}
	p, err := s.store.ProtocolByID(id)
	if err != nil || p.Kind != "workout" {
		s.fail(w, 404, "no such workout")
		return
	}
	items, err := s.store.WorkoutItems(id)
	if err != nil || len(items) == 0 {
		s.fail(w, 404, "this workout has no exercises yet")
		return
	}
	pl := plan{ProtocolID: p.ID, Name: p.Name, Rounds: p.Rounds}
	for _, it := range items {
		pl.Items = append(pl.Items, planItem{Position: it.Position, Label: it.Label, Unit: it.Unit(), Target: it.Target(), Rest: it.RestSecs, LoadKg: it.LoadKg, Slug: it.ExerciseSlug})
	}
	b, _ := json.Marshal(pl)
	s.render(w, "play", map[string]any{"Title": p.Name, "Protocol": s.card(*p), "PlanJSON": template.JS(b), "Player": true})
}

type createSessionRequest struct {
	ProtocolID *int64 `json:"protocol_id"`
	StartedAt  string `json:"started_at"`
	TotalSecs  int    `json:"total_secs"`
	Completed  bool   `json:"completed"`
	Scaled     bool   `json:"scaled"`
	Note       string `json:"note"`
	Items      []struct {
		Round    int      `json:"round"`
		Position int      `json:"position"`
		Label    string   `json:"label"`
		Unit     string   `json:"unit"`
		Target   *int     `json:"target"`
		Actual   *int     `json:"actual"`
		LoadKg   *float64 `json:"load_kg"`
	} `json:"items"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), 400)
		return
	}
	started := time.Now().UTC()
	if req.StartedAt != "" {
		t, err := time.Parse(time.RFC3339, req.StartedAt)
		if err != nil {
			http.Error(w, "bad started_at", 400)
			return
		}
		started = t.UTC()
	}
	var itemsByExercise map[int]*int64
	if req.ProtocolID != nil {
		p, err := s.store.ProtocolByID(*req.ProtocolID)
		if err != nil || p.Kind != "workout" {
			http.Error(w, "no such workout", 400)
			return
		}
		items, _ := s.store.WorkoutItems(p.ID)
		itemsByExercise = map[int]*int64{}
		for _, it := range items {
			itemsByExercise[it.Position] = it.ExerciseID
		}
	}
	id, err := s.store.CreateSession(store.Session{
		Kind: "workout", ProtocolID: req.ProtocolID, StartedAt: started, Note: req.Note,
		Completed: req.Completed, Scaled: req.Scaled,
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	wr := store.WorkoutResult{SessionID: id}
	if req.TotalSecs > 0 {
		wr.TotalSecs = &req.TotalSecs
	}
	if err := s.store.InsertWorkoutResult(wr); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var se []store.SessionExercise
	for _, it := range req.Items {
		unit := it.Unit
		if unit != "secs" {
			unit = "reps"
		}
		row := store.SessionExercise{SessionID: id, Round: it.Round, Position: it.Position, Label: it.Label, Unit: unit,
			TargetValue: it.Target, ActualValue: it.Actual, LoadKg: it.LoadKg}
		if itemsByExercise != nil {
			row.ExerciseID = itemsByExercise[it.Position]
		}
		se = append(se, row)
	}
	if len(se) > 0 {
		if err := s.store.InsertSessionExercises(id, se); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id, "rate_url": fmt.Sprintf("/session/%d/rate", id)})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, 400, "bad id")
		return
	}
	sess, err := s.store.SessionByID(id)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, 404, "no such session")
		return
	}
	if err != nil {
		s.fail(w, 500, err.Error())
		return
	}
	data := map[string]any{"Title": "Session", "S": s.row(*sess)}
	if sess.Kind == "route" {
		data["Route"], _ = s.store.RouteMetricsBySession(id)
	}
	if sess.Kind == "workout" {
		data["Result"], _ = s.store.WorkoutResultBySession(id)
		data["Items"], _ = s.store.SessionExercises(id)
	}
	s.render(w, "session", data)
}

func (s *Server) handleRateForm(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, 400, "bad id")
		return
	}
	sess, err := s.store.SessionByID(id)
	if err != nil {
		s.fail(w, 404, "no such session")
		return
	}
	s.render(w, "rate", map[string]any{"Title": "How did it feel?", "S": s.row(*sess)})
}

func (s *Server) handleRate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, 400, "bad id")
		return
	}
	effort, _ := strconv.Atoi(r.FormValue("effort"))
	if effort < 1 || effort > 5 {
		s.fail(w, 400, "pick an effort from 1 to 5")
		return
	}
	if err := s.store.RateSession(id, effort, strings.TrimSpace(r.FormValue("note"))); err != nil {
		s.fail(w, 404, "no such session")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/session/%d", id), http.StatusSeeOther)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSessions(store.SessionFilter{Limit: 100})
	if err != nil {
		s.fail(w, 500, err.Error())
		return
	}
	s.render(w, "sessions", map[string]any{"Title": "History", "Sessions": s.rows(list)})
}

type protocolStats struct {
	Comparable int
	MedianSecs int
	BestSecs   int
}

func (s *Server) handleProtocol(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, 400, "bad id")
		return
	}
	p, err := s.store.ProtocolByID(id)
	if err != nil {
		s.fail(w, 404, "no such protocol")
		return
	}
	list, _ := s.store.ListSessions(store.SessionFilter{ProtocolID: &id})
	rows := s.rows(list)
	var times []int
	for _, row := range rows {
		if !row.Comparable {
			continue
		}
		switch p.Kind {
		case "workout":
			if wr, err := s.store.WorkoutResultBySession(row.ID); err == nil && wr.TotalSecs != nil {
				times = append(times, *wr.TotalSecs)
			}
		case "route":
			if m, err := s.store.RouteMetricsBySession(row.ID); err == nil && m.MovingS > 0 {
				times = append(times, m.MovingS)
			}
		}
	}
	var st protocolStats
	if len(times) > 0 {
		sort.Ints(times)
		st = protocolStats{Comparable: len(times), MedianSecs: times[len(times)/2], BestSecs: times[0]}
	}
	s.render(w, "protocol", map[string]any{"Title": p.Name, "P": s.card(*p), "Sessions": rows, "Stats": st})
}

// ---- helpers ---------------------------------------------------------------

func hms(secs int) string {
	h, m, s := secs/3600, (secs%3600)/60, secs%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
