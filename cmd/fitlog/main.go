// fitlog is the command-line interface for Fit Log, stage 1: core model,
// import, and rederive. No web layer yet.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/giobuilds/sturdy-goggles/internal/config"
	"github.com/giobuilds/sturdy-goggles/internal/db"
	"github.com/giobuilds/sturdy-goggles/internal/importer"
	"github.com/giobuilds/sturdy-goggles/internal/seed"
	"github.com/giobuilds/sturdy-goggles/internal/store"
)

const usage = `fitlog: a personal training log

usage: fitlog <command> [flags]

  init                              create the database and data directory
  seed                              load the exercise library (idempotent)
  protocol add|list                 named, repeatable routes and workouts
  import [flags] <file>...          import .gpx / .fit recordings as route sessions
  workout log [flags]               log a workout session
  session list|rate|show            browse and rate sessions
  exercise list [--discipline D]    browse the exercise library
  rederive                          recompute every route session's metrics from stored files

Environment: FITLOG_DATA (default ./data), FITLOG_MOVING_SPEED_MPS, FITLOG_ASCENT_THRESHOLD_M.
`

type app struct {
	cfg   config.Config
	store *store.Store
	imp   *importer.Importer
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fitlog:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if args[0] == "init" {
		return cmdInit(cfg)
	}
	dbPath := filepath.Join(cfg.DataDir, "fitlog.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no database at %s (run `fitlog init`)", dbPath)
	}
	d, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	a := &app{cfg: cfg, store: store.New(d)}
	a.imp = &importer.Importer{Store: a.store, DataDir: cfg.DataDir, Params: cfg.Track}

	switch args[0] {
	case "seed":
		return a.cmdSeed()
	case "protocol":
		return a.cmdProtocol(args[1:])
	case "import":
		return a.cmdImport(args[1:])
	case "workout":
		return a.cmdWorkout(args[1:])
	case "session":
		return a.cmdSession(args[1:])
	case "exercise":
		return a.cmdExercise(args[1:])
	case "rederive":
		return a.cmdRederive()
	}
	return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
}

func cmdInit(cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join(cfg.DataDir, "fitlog.db")
	d, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	n, err := seed.Load(store.New(d))
	if err != nil {
		return err
	}
	fmt.Printf("initialised %s (%d exercises seeded)\n", dbPath, n)
	return nil
}

func (a *app) cmdSeed() error {
	n, err := seed.Load(a.store)
	if err != nil {
		return err
	}
	fmt.Printf("seeded %d exercises\n", n)
	return nil
}

// ---- protocol --------------------------------------------------------------

func (a *app) cmdProtocol(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fitlog protocol add|list")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("protocol add", flag.ContinueOnError)
		kind := fs.String("kind", "route", "route or workout")
		mode := fs.String("mode", "", "route only: walk, ride, run")
		name := fs.String("name", "", "unique name")
		notes := fs.String("notes", "", "free text")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return errors.New("--name is required")
		}
		id, err := a.store.CreateProtocol(store.Protocol{Kind: *kind, Mode: *mode, Name: *name, Notes: *notes})
		if err != nil {
			return err
		}
		fmt.Printf("protocol %d: %s %q\n", id, describeKind(*kind, *mode), *name)
		return nil
	case "list":
		ps, err := a.store.ListProtocols(false)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tNAME\tNOTES")
		for _, p := range ps {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", p.ID, describeKind(p.Kind, p.Mode), p.Name, p.Notes)
		}
		return w.Flush()
	}
	return fmt.Errorf("unknown protocol subcommand %q", args[0])
}

func describeKind(kind, mode string) string {
	if mode != "" {
		return kind + "/" + mode
	}
	return kind
}

// ---- import ----------------------------------------------------------------

func (a *app) cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	mode := fs.String("mode", "", "walk, ride or run (default: from the file, else walk)")
	protoName := fs.String("protocol", "", "assign to this route protocol by name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("usage: fitlog import [--mode M] [--protocol NAME] <file>...")
	}
	opts := importer.Options{Mode: *mode}
	if *protoName != "" {
		p, err := a.store.ProtocolByName(*protoName)
		if err != nil {
			return fmt.Errorf("protocol %q: %w", *protoName, err)
		}
		opts.ProtocolID = &p.ID
	}
	failed := 0
	for _, f := range fs.Args() {
		r, err := a.imp.ImportFile(f, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", filepath.Base(f), err)
			failed++
			continue
		}
		if r.Duplicate {
			fmt.Printf("%s: already imported as session %d\n", filepath.Base(f), r.SessionID)
			continue
		}
		m := r.Metrics
		fmt.Printf("%s: session %d (%s, %s)\n", filepath.Base(f), r.SessionID, r.Mode, r.SourceKind)
		fmt.Printf("  %.2f km  elapsed %s  moving %s  ascent %.0f m  %d points",
			m.DistanceM/1000, hms(m.ElapsedS), hms(m.MovingS), m.AscentM, m.Points)
		if m.AvgHR > 0 {
			fmt.Printf("  HR avg %d max %d", m.AvgHR, m.MaxHR)
		}
		fmt.Println()
		if d := r.Device; d != nil && d.DistanceM > 0 {
			fmt.Printf("  device: %.2f km  timer %s  ascent %.0f m  HR avg %d max %d\n",
				d.DistanceM/1000, hms(int(d.TimerS)), d.AscentM, d.AvgHR, d.MaxHR)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files failed", failed, fs.NArg())
	}
	return nil
}

// ---- workout ---------------------------------------------------------------

func (a *app) cmdWorkout(args []string) error {
	if len(args) == 0 || args[0] != "log" {
		return errors.New("usage: fitlog workout log --protocol NAME [--secs N] [--rounds N] [--effort 1-5] [--note ..] [--scaled] [--incomplete] [--at RFC3339]")
	}
	fs := flag.NewFlagSet("workout log", flag.ContinueOnError)
	protoName := fs.String("protocol", "", "workout protocol name (empty for a one-off)")
	secs := fs.Int("secs", 0, "total time to complete, seconds")
	rounds := fs.Int("rounds", 0, "rounds completed")
	effort := fs.Int("effort", 0, "1-5, leave 0 to rate later")
	note := fs.String("note", "", "free text")
	scaled := fs.Bool("scaled", false, "work was modified from the prescription")
	incomplete := fs.Bool("incomplete", false, "stopped early")
	at := fs.String("at", "", "start time, RFC 3339 (default now)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	sess := store.Session{Kind: "workout", StartedAt: time.Now().UTC(), Note: *note, Completed: !*incomplete, Scaled: *scaled}
	if *at != "" {
		t, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			return fmt.Errorf("--at: %w", err)
		}
		sess.StartedAt = t.UTC()
	}
	if *effort != 0 {
		sess.Effort = effort
	}
	if *protoName != "" {
		p, err := a.store.ProtocolByName(*protoName)
		if err != nil {
			return fmt.Errorf("protocol %q: %w", *protoName, err)
		}
		if p.Kind != "workout" {
			return fmt.Errorf("protocol %q is a %s, not a workout", p.Name, p.Kind)
		}
		sess.ProtocolID = &p.ID
	}
	id, err := a.store.CreateSession(sess)
	if err != nil {
		return err
	}
	res := store.WorkoutResult{SessionID: id}
	if *secs > 0 {
		res.TotalSecs = secs
	}
	if *rounds > 0 {
		res.Rounds = rounds
	}
	if err := a.store.InsertWorkoutResult(res); err != nil {
		return err
	}
	fmt.Printf("workout session %d logged\n", id)
	return nil
}

// ---- session ---------------------------------------------------------------

func (a *app) cmdSession(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fitlog session list|rate|show")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("session list", flag.ContinueOnError)
		protoName := fs.String("protocol", "", "filter by protocol name")
		kind := fs.String("kind", "", "route, workout or activity")
		limit := fs.Int("n", 30, "max rows")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		f := store.SessionFilter{Kind: *kind, Limit: *limit}
		if *protoName != "" {
			p, err := a.store.ProtocolByName(*protoName)
			if err != nil {
				return fmt.Errorf("protocol %q: %w", *protoName, err)
			}
			f.ProtocolID = &p.ID
		}
		sessions, err := a.store.ListSessions(f)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTARTED\tKIND\tPROTOCOL\tRESULT\tEFFORT\tFLAGS\tNOTE")
		for _, s := range sessions {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.StartedAt.Local().Format("2006-01-02 15:04"), describeKind(s.Kind, s.Mode),
				a.protocolName(s.ProtocolID), a.resultSummary(s), effortStr(s.Effort), flags(s), s.Note)
		}
		return w.Flush()
	case "rate":
		// Positional id first, then flags: `fitlog session rate 12 --effort 3`.
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return errors.New("usage: fitlog session rate <id> --effort N [--note ..]")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("session id: %w", err)
		}
		fs := flag.NewFlagSet("session rate", flag.ContinueOnError)
		effort := fs.Int("effort", 0, "1-5")
		note := fs.String("note", "", "free text")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *effort < 1 || *effort > 5 {
			return errors.New("--effort must be 1-5")
		}
		return a.store.RateSession(id, *effort, *note)
	case "show":
		if len(args) != 2 {
			return errors.New("usage: fitlog session show <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return err
		}
		s, err := a.store.SessionByID(id)
		if err != nil {
			return err
		}
		fmt.Printf("session %d  %s  %s  protocol: %s\n", s.ID, s.StartedAt.Local().Format(time.RFC1123), describeKind(s.Kind, s.Mode), a.protocolName(s.ProtocolID))
		fmt.Printf("effort: %s  flags: %s  note: %s\n", effortStr(s.Effort), flags(*s), s.Note)
		if m, err := a.store.RouteMetricsBySession(id); err == nil {
			fmt.Printf("%.2f km  elapsed %s  moving %s  ascent %.0f m  %d points  (source %s %s, derive v%d)\n",
				m.DistanceM/1000, hms(m.ElapsedS), hms(m.MovingS), m.AscentM, m.Points, m.SourceKind, m.SourceHash[:12], m.DeriveVersion)
			if m.AvgHR > 0 {
				fmt.Printf("HR avg %d max %d\n", m.AvgHR, m.MaxHR)
			}
		}
		if w, err := a.store.WorkoutResultBySession(id); err == nil {
			fmt.Printf("result: %s\n", workoutResultStr(w))
		}
		return nil
	}
	return fmt.Errorf("unknown session subcommand %q", args[0])
}

// ---- exercise --------------------------------------------------------------

func (a *app) cmdExercise(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: fitlog exercise list [--discipline D]")
	}
	fs := flag.NewFlagSet("exercise list", flag.ContinueOnError)
	disc := fs.String("discipline", "", "pilates, calisthenics, hiit or mobility")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	ex, err := a.store.ListExercises(*disc)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SLUG\tDISCIPLINE\tLVL\tUNIT\tEQUIP\tREGRESSION\tTAGS")
	for _, e := range ex {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\t%s\n", e.Slug, e.Discipline, e.Difficulty, e.Unit, e.Equipment, e.RegressionOf, strings.Join(e.Tags, ","))
	}
	return w.Flush()
}

// ---- rederive --------------------------------------------------------------

func (a *app) cmdRederive() error {
	n, err := a.imp.Rederive()
	if err != nil {
		return err
	}
	fmt.Printf("re-derived %d route sessions\n", n)
	return nil
}

// ---- formatting ------------------------------------------------------------

func (a *app) protocolName(id *int64) string {
	if id == nil {
		return "-"
	}
	p, err := a.store.ProtocolByID(*id)
	if err != nil {
		return "?"
	}
	return p.Name
}

func (a *app) resultSummary(s store.Session) string {
	switch s.Kind {
	case "route":
		m, err := a.store.RouteMetricsBySession(s.ID)
		if err != nil {
			return "-"
		}
		out := fmt.Sprintf("%.2f km / %s", m.DistanceM/1000, hms(m.MovingS))
		if m.AvgHR > 0 {
			out += fmt.Sprintf(" / HR %d", m.AvgHR)
		}
		return out
	case "workout":
		w, err := a.store.WorkoutResultBySession(s.ID)
		if err != nil {
			return "-"
		}
		return workoutResultStr(w)
	}
	return "-"
}

func workoutResultStr(w *store.WorkoutResult) string {
	var parts []string
	if w.TotalSecs != nil {
		parts = append(parts, hms(*w.TotalSecs))
	}
	if w.Rounds != nil {
		parts = append(parts, fmt.Sprintf("%d rounds", *w.Rounds))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " / ")
}

func effortStr(e *int) string {
	if e == nil {
		return "unrated"
	}
	return strconv.Itoa(*e)
}

func flags(s store.Session) string {
	var f []string
	if s.Scaled {
		f = append(f, "scaled")
	}
	if !s.Completed {
		f = append(f, "incomplete")
	}
	if len(f) == 0 {
		return "-"
	}
	return strings.Join(f, ",")
}

func hms(secs int) string {
	h, m, s := secs/3600, (secs%3600)/60, secs%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
