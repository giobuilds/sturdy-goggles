// Package store is the persistence layer over the SQLite schema. It knows
// nothing about files or parsing; see importer for that.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/giobuilds/sturdy-goggles/internal/track"
)

var ErrNotFound = errors.New("not found")

const tsLayout = time.RFC3339

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

func now() string { return time.Now().UTC().Format(tsLayout) }

func parseTS(s string) time.Time {
	t, _ := time.Parse(tsLayout, s)
	return t
}

// ---- protocols -------------------------------------------------------------

type Protocol struct {
	ID        int64
	Kind      string // route | workout
	Mode      string // route only: walk | ride | run
	Name      string
	Notes     string
	Rounds    int  // workout only; circuit rounds, at least 1
	Starter   bool // shipped with the app
	Archived  bool
	CreatedAt time.Time
}

func (s *Store) CreateProtocol(p Protocol) (int64, error) {
	if p.Kind != "route" && p.Kind != "workout" {
		return 0, fmt.Errorf("protocol kind must be route or workout")
	}
	if p.Kind == "workout" && p.Mode != "" {
		return 0, fmt.Errorf("mode applies to route protocols only")
	}
	if p.Rounds < 1 {
		p.Rounds = 1
	}
	res, err := s.DB.Exec(`INSERT INTO protocols(kind, mode, name, notes, rounds, starter, created_at) VALUES (?,?,?,?,?,?,?)`,
		p.Kind, nullStr(p.Mode), p.Name, p.Notes, p.Rounds, boolInt(p.Starter), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ProtocolByName(name string) (*Protocol, error) {
	row := s.DB.QueryRow(`SELECT id, kind, COALESCE(mode,''), name, notes, rounds, starter, archived, created_at FROM protocols WHERE name = ?`, name)
	return scanProtocol(row)
}

func (s *Store) ProtocolByID(id int64) (*Protocol, error) {
	row := s.DB.QueryRow(`SELECT id, kind, COALESCE(mode,''), name, notes, rounds, starter, archived, created_at FROM protocols WHERE id = ?`, id)
	return scanProtocol(row)
}

func (s *Store) ListProtocols(includeArchived bool) ([]Protocol, error) {
	q := `SELECT id, kind, COALESCE(mode,''), name, notes, rounds, starter, archived, created_at FROM protocols`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	rows, err := s.DB.Query(q + ` ORDER BY kind, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Protocol
	for rows.Next() {
		p, err := scanProtocol(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanProtocol(r scanner) (*Protocol, error) {
	var p Protocol
	var starter, archived int
	var created string
	err := r.Scan(&p.ID, &p.Kind, &p.Mode, &p.Name, &p.Notes, &p.Rounds, &starter, &archived, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Starter = starter == 1
	p.Archived = archived == 1
	p.CreatedAt = parseTS(created)
	return &p, nil
}

// UpdateProtocol rewrites the editable fields: name, notes, rounds, archived.
func (s *Store) UpdateProtocol(p Protocol) error {
	if p.Rounds < 1 {
		p.Rounds = 1
	}
	res, err := s.DB.Exec(`UPDATE protocols SET name=?, notes=?, rounds=?, archived=? WHERE id=?`,
		p.Name, p.Notes, p.Rounds, boolInt(p.Archived), p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- workout items (a workout protocol's prescription) --------------------

type WorkoutItem struct {
	ID           int64
	ProtocolID   int64
	Position     int
	ExerciseID   *int64
	ExerciseSlug string // "" when free text
	Label        string
	TargetReps   *int
	TargetSecs   *int
	RestSecs     int
	LoadKg       *float64
}

// Unit is "secs" for timed items, otherwise "reps".
func (w WorkoutItem) Unit() string {
	if w.TargetSecs != nil {
		return "secs"
	}
	return "reps"
}

func (w WorkoutItem) Target() int {
	if w.TargetSecs != nil {
		return *w.TargetSecs
	}
	if w.TargetReps != nil {
		return *w.TargetReps
	}
	return 0
}

// ReplaceWorkoutItems sets the full prescription for a workout protocol.
func (s *Store) ReplaceWorkoutItems(protocolID int64, items []WorkoutItem) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM workout_items WHERE protocol_id = ?`, protocolID); err != nil {
		return err
	}
	for i, it := range items {
		if (it.TargetReps == nil) == (it.TargetSecs == nil) {
			return fmt.Errorf("item %d (%s): exactly one of reps or secs required", i+1, it.Label)
		}
		if _, err := tx.Exec(`INSERT INTO workout_items(protocol_id, position, exercise_id, label, target_reps, target_secs, rest_secs, load_kg)
			VALUES (?,?,?,?,?,?,?,?)`, protocolID, i+1, it.ExerciseID, it.Label, it.TargetReps, it.TargetSecs, it.RestSecs, it.LoadKg); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) WorkoutItems(protocolID int64) ([]WorkoutItem, error) {
	rows, err := s.DB.Query(`SELECT w.id, w.protocol_id, w.position, w.exercise_id, COALESCE(e.slug,''), w.label,
		w.target_reps, w.target_secs, w.rest_secs, w.load_kg
		FROM workout_items w LEFT JOIN exercises e ON e.id = w.exercise_id
		WHERE w.protocol_id = ? ORDER BY w.position`, protocolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkoutItem
	for rows.Next() {
		var it WorkoutItem
		var exID sql.NullInt64
		var reps, secs sql.NullInt64
		var load sql.NullFloat64
		if err := rows.Scan(&it.ID, &it.ProtocolID, &it.Position, &exID, &it.ExerciseSlug, &it.Label, &reps, &secs, &it.RestSecs, &load); err != nil {
			return nil, err
		}
		if exID.Valid {
			it.ExerciseID = &exID.Int64
		}
		if reps.Valid {
			v := int(reps.Int64)
			it.TargetReps = &v
		}
		if secs.Valid {
			v := int(secs.Int64)
			it.TargetSecs = &v
		}
		if load.Valid {
			it.LoadKg = &load.Float64
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ---- sessions --------------------------------------------------------------

type Session struct {
	ID         int64
	Kind       string // route | workout | activity
	Mode       string // route only
	ProtocolID *int64
	StartedAt  time.Time
	Effort     *int // nil until rated
	Note       string
	Completed  bool
	Scaled     bool
	CreatedAt  time.Time
}

func (s *Store) CreateSession(sess Session) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO sessions(kind, mode, protocol_id, started_at, effort, note, completed, scaled, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		sess.Kind, nullStr(sess.Mode), sess.ProtocolID, sess.StartedAt.UTC().Format(tsLayout),
		sess.Effort, sess.Note, boolInt(sess.Completed), boolInt(sess.Scaled), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RateSession sets the subjective fields. Passing effort 0 leaves it unrated.
func (s *Store) RateSession(id int64, effort int, note string) error {
	var eff any
	if effort != 0 {
		eff = effort
	}
	res, err := s.DB.Exec(`UPDATE sessions SET effort = ?, note = ? WHERE id = ?`, eff, note, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SessionByID(id int64) (*Session, error) {
	row := s.DB.QueryRow(sessionSelect+` WHERE id = ?`, id)
	return scanSession(row)
}

// SessionBySourceHash finds the route session already imported from a file.
func (s *Store) SessionBySourceHash(hash string) (*Session, error) {
	row := s.DB.QueryRow(sessionSelect+` WHERE id = (SELECT session_id FROM route_metrics WHERE source_hash = ?)`, hash)
	return scanSession(row)
}

type SessionFilter struct {
	ProtocolID *int64
	Kind       string
	Limit      int
}

func (s *Store) ListSessions(f SessionFilter) ([]Session, error) {
	q := sessionSelect + ` WHERE 1=1`
	var args []any
	if f.ProtocolID != nil {
		q += ` AND protocol_id = ?`
		args = append(args, *f.ProtocolID)
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	q += ` ORDER BY started_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

const sessionSelect = `SELECT id, kind, COALESCE(mode,''), protocol_id, started_at, effort, note, completed, scaled, created_at FROM sessions`

func scanSession(r scanner) (*Session, error) {
	var sess Session
	var pid sql.NullInt64
	var effort sql.NullInt64
	var completed, scaled int
	var started, created string
	err := r.Scan(&sess.ID, &sess.Kind, &sess.Mode, &pid, &started, &effort, &sess.Note, &completed, &scaled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if pid.Valid {
		sess.ProtocolID = &pid.Int64
	}
	if effort.Valid {
		e := int(effort.Int64)
		sess.Effort = &e
	}
	sess.Completed = completed == 1
	sess.Scaled = scaled == 1
	sess.StartedAt = parseTS(started)
	sess.CreatedAt = parseTS(created)
	return &sess, nil
}

// ---- route metrics ---------------------------------------------------------

type RouteMetrics struct {
	SessionID  int64
	SourceKind string // gpx | fit
	SourceHash string
	track.Metrics
	DerivedAt     time.Time
	DeriveVersion int
}

func (s *Store) InsertRouteMetrics(m RouteMetrics) error {
	_, err := s.DB.Exec(`INSERT INTO route_metrics(session_id, source_kind, source_hash, points, distance_m, elapsed_s, moving_s,
		ascent_m, start_lat, start_lon, end_lat, end_lon, avg_hr, max_hr, derived_at, derive_version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.SessionID, m.SourceKind, m.SourceHash, m.Points, m.DistanceM, m.ElapsedS, m.MovingS,
		m.AscentM, m.StartLat, m.StartLon, m.EndLat, m.EndLon, m.AvgHR, m.MaxHR, now(), m.DeriveVersion)
	return err
}

// UpdateRouteMetrics rewrites only the derived columns. Source identity and
// the session's subjective fields are untouched by design.
func (s *Store) UpdateRouteMetrics(sessionID int64, m track.Metrics, version int) error {
	_, err := s.DB.Exec(`UPDATE route_metrics SET points=?, distance_m=?, elapsed_s=?, moving_s=?, ascent_m=?,
		start_lat=?, start_lon=?, end_lat=?, end_lon=?, avg_hr=?, max_hr=?, derived_at=?, derive_version=?
		WHERE session_id = ?`,
		m.Points, m.DistanceM, m.ElapsedS, m.MovingS, m.AscentM,
		m.StartLat, m.StartLon, m.EndLat, m.EndLon, m.AvgHR, m.MaxHR, now(), version, sessionID)
	return err
}

func (s *Store) RouteMetricsBySession(sessionID int64) (*RouteMetrics, error) {
	row := s.DB.QueryRow(routeMetricsSelect+` WHERE session_id = ?`, sessionID)
	m, err := scanRouteMetrics(row)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Store) AllRouteMetrics() ([]RouteMetrics, error) {
	rows, err := s.DB.Query(routeMetricsSelect + ` ORDER BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouteMetrics
	for rows.Next() {
		m, err := scanRouteMetrics(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

const routeMetricsSelect = `SELECT session_id, source_kind, source_hash, points, distance_m, elapsed_s, moving_s, ascent_m,
	start_lat, start_lon, end_lat, end_lon, avg_hr, max_hr, derived_at, derive_version FROM route_metrics`

func scanRouteMetrics(r scanner) (*RouteMetrics, error) {
	var m RouteMetrics
	var derived string
	err := r.Scan(&m.SessionID, &m.SourceKind, &m.SourceHash, &m.Points, &m.DistanceM, &m.ElapsedS, &m.MovingS, &m.AscentM,
		&m.StartLat, &m.StartLon, &m.EndLat, &m.EndLon, &m.AvgHR, &m.MaxHR, &derived, &m.DeriveVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.DerivedAt = parseTS(derived)
	return &m, nil
}

// ---- workout results -------------------------------------------------------

type WorkoutResult struct {
	SessionID int64
	TotalSecs *int
	Rounds    *int
}

func (s *Store) InsertWorkoutResult(w WorkoutResult) error {
	_, err := s.DB.Exec(`INSERT INTO workout_results(session_id, total_secs, rounds) VALUES (?,?,?)`,
		w.SessionID, w.TotalSecs, w.Rounds)
	return err
}

func (s *Store) WorkoutResultBySession(sessionID int64) (*WorkoutResult, error) {
	var w WorkoutResult
	var secs, rounds sql.NullInt64
	err := s.DB.QueryRow(`SELECT session_id, total_secs, rounds FROM workout_results WHERE session_id = ?`, sessionID).
		Scan(&w.SessionID, &secs, &rounds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if secs.Valid {
		v := int(secs.Int64)
		w.TotalSecs = &v
	}
	if rounds.Valid {
		v := int(rounds.Int64)
		w.Rounds = &v
	}
	return &w, nil
}

// ---- exercises -------------------------------------------------------------

type Exercise struct {
	ID           int64
	Slug         string
	Name         string
	Discipline   string
	Difficulty   int
	Equipment    string
	Unit         string
	RegressionOf string // slug, "" if none
	Source       string
	Notes        string
	Tags         []string
}

// UpsertExercise inserts or updates by slug and replaces its tags. Regression
// links are resolved by slug and must already exist (seed files are ordered).
func (s *Store) UpsertExercise(e Exercise) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var regID any
	if e.RegressionOf != "" {
		var id int64
		if err := tx.QueryRow(`SELECT id FROM exercises WHERE slug = ?`, e.RegressionOf).Scan(&id); err != nil {
			return 0, fmt.Errorf("exercise %s: regression_of %q not found", e.Slug, e.RegressionOf)
		}
		regID = id
	}
	_, err = tx.Exec(`INSERT INTO exercises(slug, name, discipline, difficulty, equipment, unit, regression_of, source, notes)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(slug) DO UPDATE SET name=excluded.name, discipline=excluded.discipline, difficulty=excluded.difficulty,
		equipment=excluded.equipment, unit=excluded.unit, regression_of=excluded.regression_of, source=excluded.source, notes=excluded.notes`,
		e.Slug, e.Name, e.Discipline, e.Difficulty, e.Equipment, e.Unit, regID, e.Source, e.Notes)
	if err != nil {
		return 0, fmt.Errorf("exercise %s: %w", e.Slug, err)
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM exercises WHERE slug = ?`, e.Slug).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM exercise_tags WHERE exercise_id = ?`, id); err != nil {
		return 0, err
	}
	for _, tag := range e.Tags {
		if _, err := tx.Exec(`INSERT INTO exercise_tags(exercise_id, tag) VALUES (?,?)`, id, tag); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (s *Store) ListExercises(discipline string) ([]Exercise, error) {
	q := `SELECT e.id, e.slug, e.name, e.discipline, e.difficulty, e.equipment, e.unit, COALESCE(r.slug,''), e.source, e.notes
		FROM exercises e LEFT JOIN exercises r ON r.id = e.regression_of`
	var args []any
	if discipline != "" {
		q += ` WHERE e.discipline = ?`
		args = append(args, discipline)
	}
	rows, err := s.DB.Query(q+` ORDER BY e.discipline, e.difficulty, e.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Exercise
	for rows.Next() {
		var e Exercise
		if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Discipline, &e.Difficulty, &e.Equipment, &e.Unit, &e.RegressionOf, &e.Source, &e.Notes); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		tags, err := s.exerciseTags(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Tags = tags
	}
	return out, nil
}

func (s *Store) exerciseTags(id int64) ([]string, error) {
	rows, err := s.DB.Query(`SELECT tag FROM exercise_tags WHERE exercise_id = ? ORDER BY tag`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ---- session exercises (what was actually done) ---------------------------

type SessionExercise struct {
	SessionID   int64
	Round       int
	Position    int
	ExerciseID  *int64
	Label       string
	Unit        string // reps | secs
	TargetValue *int
	ActualValue *int
	LoadKg      *float64
}

func (s *Store) InsertSessionExercises(sessionID int64, items []SessionExercise) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO session_exercises(session_id, round, position, exercise_id, label, unit, target_value, actual_value, load_kg)
			VALUES (?,?,?,?,?,?,?,?,?)`, sessionID, it.Round, it.Position, it.ExerciseID, it.Label, it.Unit, it.TargetValue, it.ActualValue, it.LoadKg); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SessionExercises(sessionID int64) ([]SessionExercise, error) {
	rows, err := s.DB.Query(`SELECT session_id, round, position, exercise_id, label, unit, target_value, actual_value, load_kg
		FROM session_exercises WHERE session_id = ? ORDER BY round, position`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionExercise
	for rows.Next() {
		var it SessionExercise
		var exID, target, actual sql.NullInt64
		var load sql.NullFloat64
		if err := rows.Scan(&it.SessionID, &it.Round, &it.Position, &exID, &it.Label, &it.Unit, &target, &actual, &load); err != nil {
			return nil, err
		}
		if exID.Valid {
			it.ExerciseID = &exID.Int64
		}
		if target.Valid {
			v := int(target.Int64)
			it.TargetValue = &v
		}
		if actual.Valid {
			v := int(actual.Int64)
			it.ActualValue = &v
		}
		if load.Valid {
			it.LoadKg = &load.Float64
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ---- consistency -----------------------------------------------------------

type WeekCount struct {
	Start    time.Time // local Monday 00:00
	Total    int
	Workouts int
	Routes   int
}

// WeeklyCounts returns sessions per local week for the last n weeks, oldest
// first, the current week last. Every kind counts: consistency is about
// showing up, not about what was done.
func (s *Store) WeeklyCounts(loc *time.Location, n int, at time.Time) ([]WeekCount, error) {
	thisMonday := weekStart(at.In(loc))
	first := thisMonday.AddDate(0, 0, -7*(n-1))
	rows, err := s.DB.Query(`SELECT started_at, kind FROM sessions WHERE started_at >= ? ORDER BY started_at`,
		first.UTC().Format(tsLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	weeks := make([]WeekCount, n)
	for i := range weeks {
		weeks[i].Start = first.AddDate(0, 0, 7*i)
	}
	for rows.Next() {
		var started, kind string
		if err := rows.Scan(&started, &kind); err != nil {
			return nil, err
		}
		t := parseTS(started).In(loc)
		idx := int(weekStart(t).Sub(first).Hours() / (24 * 7))
		if idx < 0 || idx >= n {
			continue
		}
		weeks[idx].Total++
		switch kind {
		case "workout":
			weeks[idx].Workouts++
		case "route":
			weeks[idx].Routes++
		}
	}
	return weeks, rows.Err()
}

func weekStart(t time.Time) time.Time {
	wd := int(t.Weekday()) // Sunday = 0
	if wd == 0 {
		wd = 7
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -(wd - 1))
}

func (s *Store) UnratedSessions() ([]Session, error) {
	rows, err := s.DB.Query(sessionSelect + ` WHERE effort IS NULL ORDER BY started_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

func (s *Store) ExerciseBySlug(slug string) (*Exercise, error) {
	var e Exercise
	err := s.DB.QueryRow(`SELECT e.id, e.slug, e.name, e.discipline, e.difficulty, e.equipment, e.unit, COALESCE(r.slug,''), e.source, e.notes
		FROM exercises e LEFT JOIN exercises r ON r.id = e.regression_of WHERE e.slug = ?`, slug).
		Scan(&e.ID, &e.Slug, &e.Name, &e.Discipline, &e.Difficulty, &e.Equipment, &e.Unit, &e.RegressionOf, &e.Source, &e.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Tags, err = s.exerciseTags(e.ID)
	return &e, err
}

// ---- helpers ---------------------------------------------------------------

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
