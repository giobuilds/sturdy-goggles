# Fit Log — Design Brief

**Status:** v2 spec, supersedes "Walk Log — Design Brief"
**Author:** Gio
**Target:** self-hosted web app on `citadel`, reached over Tailscale

Changes from v1: covers workouts as well as walks; hosting moved from the retired `workshop` to `citadel`; adds a progression engine.

---

## 1. Purpose

A personal training log that **answers questions** rather than accumulating numbers, and proposes what to do next based on what you've actually done.

The test for any feature: does it help answer something like —

- "Is this getting easier?"
- "Was that slow, or did it just feel slow?"
- "Have I been consistent this month?"
- "What should I aim for next time?"

If a feature doesn't serve a question of that shape, it doesn't belong.

### Non-goals

- No social features, sharing, leaderboards, or accounts beyond a single user.
- No live GPS tracking in-app. Recording is delegated to any app that exports GPX. Reference recorder: **Open GPX Tracker** (iOS, GPL-3.0). No dependence on recorder-specific quirks — GPX in, nothing else.
- No calorie estimation, macro tracking, or weight logging.
- **No generated training programmes.** The progression engine proposes the next target for a workout you have already chosen to do. It does not decide what you should train, when to rest, or how to periodise. See §6.
- No public internet exposure. The app is reachable on the LAN and over Tailscale, and nowhere else.

---

## 2. Core concepts

The central abstraction, and the reason walks and workouts belong in one app:

**Protocol** — a named, repeatable thing you do, whose instances are compared over time.

This has two kinds:

- **Route** — a walk over named ground ("Canal loop", "Hill to the church").
- **Workout** — a named set of prescribed work ("Aphrodite", "5×10 push-ups / 15 squats").

**Session** — one instance of performing a protocol, on a date, with results and subjective data.

Everything downstream follows from this. The comparison view, the consistency view, and the progression engine all operate on *protocol → sessions over time* and are largely indifferent to which kind they're looking at. The kinds differ in exactly one place: **how a session's results arrive.** Walk metrics are derived from an uploaded GPX file. Workout results are entered by hand.

Resist any change that makes the two kinds diverge further than that.

---

## 3. Data model

### Principle: store the raw input, derive everything else

For walks, the uploaded `.gpx` is kept on disk, immutable, named by content hash, and all numeric metrics are **derived values with a pointer back to the source file**. Metric definitions will change; the history must be re-derivable rather than permanently reflecting an early bad guess. A `rederive` command recomputes every walk session's metrics from stored files.

For workouts the raw input *is* the entered result, so there is nothing to re-derive — but the same rule applies to anything computed from it (pace per rep, total volume): store the inputs, compute the rest on read.

Subjective data (effort, notes) is never derivable and must never be lost by a re-derive.

### Schema sketch (SQLite)

```sql
CREATE TABLE protocols (
    id            INTEGER PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('route','workout')),
    name          TEXT NOT NULL UNIQUE,
    notes         TEXT,
    archived      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
);

-- Workout definition: the prescribed work. Routes have no rows here.
CREATE TABLE workout_items (
    id            INTEGER PRIMARY KEY,
    protocol_id   INTEGER NOT NULL REFERENCES protocols(id),
    position      INTEGER NOT NULL,
    exercise      TEXT NOT NULL,       -- free text; no fixed taxonomy in v1
    target_reps   INTEGER,             -- nullable: some items are timed
    target_secs   INTEGER,             -- nullable: some items are counted
    UNIQUE (protocol_id, position)
);

CREATE TABLE sessions (
    id            INTEGER PRIMARY KEY,
    protocol_id   INTEGER REFERENCES protocols(id),  -- nullable: one-off efforts
    kind          TEXT NOT NULL CHECK (kind IN ('route','workout')),
    started_at    TEXT NOT NULL,
    -- subjective, user-entered, never overwritten by re-derive
    effort        INTEGER,             -- 1..5, nullable until rated
    note          TEXT,
    created_at    TEXT NOT NULL
);

-- Walk-only, all derived from the GPX
CREATE TABLE walk_metrics (
    session_id      INTEGER PRIMARY KEY REFERENCES sessions(id),
    gpx_hash        TEXT NOT NULL UNIQUE,   -- sha256, also the filename
    distance_m      REAL,
    elapsed_s       INTEGER,
    moving_s        INTEGER,
    ascent_m        REAL,
    start_lat       REAL,
    start_lon       REAL,
    end_lat         REAL,
    end_lon         REAL,
    derived_at      TEXT,
    derive_version  INTEGER                 -- bump when metric defs change
);

-- Workout-only, all user-entered
CREATE TABLE workout_results (
    session_id      INTEGER PRIMARY KEY REFERENCES sessions(id),
    total_secs      INTEGER,                -- time to complete, if timed
    rounds          INTEGER,                -- rounds completed, if applicable
    completed       INTEGER NOT NULL DEFAULT 1,  -- 0 = stopped early
    scaled          INTEGER NOT NULL DEFAULT 0   -- 0 = as prescribed, 1 = modified
);

CREATE TABLE workout_item_results (
    id              INTEGER PRIMARY KEY,
    session_id      INTEGER NOT NULL REFERENCES sessions(id),
    item_id         INTEGER REFERENCES workout_items(id),
    actual_reps     INTEGER,
    actual_secs     INTEGER
);

CREATE INDEX idx_sessions_protocol ON sessions(protocol_id, started_at);
```

`gpx_hash` as a unique constraint gives idempotent walk import for free.

`scaled` and `completed` matter more than they look: a session where you modified the work or stopped early is not comparable to one done as prescribed, and the progression engine (§6) must exclude or flag them rather than treating them as regressions.

---

## 4. Route matching

Unchanged from v1, and still the hardest part of the walk side.

General trace-to-trace matching (Fréchet distance, Hausdorff, map-matching) is genuinely hard and out of scope. Do not attempt it.

### Approach: cheap heuristics plus human confirmation

On import, score each existing route against the new trace on:

1. Distance between start points (haversine)
2. Distance between end points (haversine)
3. Ratio of total distance to that route's median distance

A route is a **candidate** if start and end are both within ~150 m of that route's median start/end and total distance is within ~15% of its median. Rank candidates by combined error.

Present the best candidate pre-selected, others one tap away, plus "new route" with a name field. **The user always confirms.** Never auto-assign silently.

An out-and-back and a loop from the same door will collide on start/end. Distance ratio separates most; confirmation catches the rest. This is fine — a debuggable heuristic the user corrects beats a clever algorithm that fails in ways nobody can explain.

Thresholds go in config, not scattered as literals.

---

## 5. Entry flows

Two flows, one shared second step. This is what determines whether the app gets used at all, and should be the most polished thing in the build.

### Walk: import

**Step 1 — Upload and match.** Accept one or more `.gpx` files. Parse, derive, hash, dedupe, match. Show derived numbers, a small track outline, and the route candidate.

**Step 2 — Rate.** Shared with workouts, below.

**iOS ingest.** A sandboxed iOS app can only hand files out via the share sheet; it cannot POST to a local server. Two mechanisms:

1. **An iOS Shortcut** accepting a GPX from the share sheet and POSTing to the import endpoint. This is the fast path. Requires the endpoint to accept plain `multipart/form-data` with no CSRF token or session cookie, authenticated by a bearer token in a header. **Design the endpoint for this from the start** — retrofitting around a cookie session is painful. Over Tailscale the token is a convenience rather than the only thing standing between your data and the internet, but keep it anyway.
2. **Plain file upload** from Safari via the Files app, as fallback and for batch backfill.

### Workout: log

Live logging during a session is out of scope for v1 — a phone-based rep counter you interact with mid-burpee is its own product. Instead:

**Step 1 — Pick and record.** Select the workout, confirm or adjust the prescribed items, enter total time and any per-item actuals. Mark `scaled` or `completed` honestly if the session didn't go to plan.

**Step 2 — Rate.** Shared.

### Step 2, shared: rate

Effort 1–5 as five large tap targets. Freeform note. Save.

Requirements for both flows:

- Step 2 completable in under 20 seconds on a phone. If it isn't, the app fails regardless of everything else.
- Narrow viewport, thumb-sized controls. Responsive mobile web, not a desktop app that technically loads on a phone.
- A session may be saved unrated and rated later. Unrated sessions surface somewhere obvious.
- Multi-file walk upload runs step 1 in batch, then queues step 2 per session.

---

## 6. Progression engine

The part that suggests what to aim for next. **Read this section before implementing it.**

### What it is

A deterministic, rules-based function from your logged history of one protocol to a suggested target for the next session. Nothing learned, nothing generative, nothing statistical beyond medians.

```
suggest(protocol, history) -> (target, reasoning[])
```

It returns the reasoning alongside the number, always, and the UI shows it. "Last time: 12:40 at effort 4. Two consecutive sessions at effort ≤3. Suggested target: 12:20 (−2%)." If you can't read why it said something, it's broken.

### What it is not

It does not decide *what* to train, *when* to rest, or how to structure a week. It has no concept of a training plan, a mesocycle, or recovery status. You choose the workout; it proposes a target for that workout only.

This boundary is deliberate and load-bearing. Generating training programmes requires exercise-science judgment that is not encoded anywhere in this system, and getting it wrong has physical consequences rather than merely producing bad output. If you later want real periodisation, it should come from a published protocol you've deliberately chosen, implemented as an explicit named ruleset — not from logic invented here.

### Rules

Start with these, in config, adjustable:

1. **Insufficient history** (fewer than 2 comparable sessions): suggest repeating the last result, or the prescribed baseline for a new protocol. No extrapolation from one data point.
2. **Comparable sessions only**: exclude any session with `scaled = 1` or `completed = 0` from trend calculations. Show them in history, but never let them drive a suggestion.
3. **Progress on sustained ease**: increase the target only after **two consecutive** comparable sessions at effort ≤ 3. Increment is small and capped — no more than 2–3% per step.
4. **Hold on hard**: after any session at effort 5, suggest the same target again. Never increase.
5. **Back off on repeated struggle**: after two consecutive sessions at effort 5, or any incomplete session, suggest a reduced target.
6. **Layoff reset**: after a gap of more than three weeks, suggest the target from before the gap, not the peak. Never resume at the best-ever result.
7. **Ceiling**: never suggest a target more than 10% beyond the best comparable result to date, however good the trend looks.

Rules 4–7 exist to make the failure mode *too cautious* rather than *too aggressive*. Keep that asymmetry when editing them.

### Presentation

Suggestions are suggestions. The UI offers the target as a prefilled, editable default, never as an instruction, and never gates or scolds. No streak-breaking language, no "you missed a session," no comparison against anyone but your own history.

---

## 7. The questions view

Per-protocol history is the payoff, and the view is shared across kinds. For a selected protocol:

- The primary result over time (moving time for routes, total time or rounds for workouts)
- Effort overlaid against result — the interesting signal is **divergence** ("slower but felt easier")
- Median and best, so a single session has context
- Scaled and incomplete sessions visibly marked, not silently mixed in

Plus a consistency view: sessions per week over the last 8–12 weeks, across both kinds. Frequency is the honest motivational metric; it doesn't require judging performance.

Language rule for all generated text, here and in §6: describe what the data shows, never evaluate the person. "4 minutes slower than your median, effort rated lower" — not "you're slowing down," not "good job."

---

## 8. Stack and deployment

- **Language:** Go. Single static binary, `modernc.org/sqlite` (pure Go, no cgo). GPX is plain XML — `encoding/xml` suffices.
- **Frontend:** server-rendered HTML, minimal JS. No SPA framework, no build pipeline.
- **Charts:** server-rendered SVG, or one small charting library.
- **Storage:** `/data/fitlog.db` and `/data/gpx/<sha256>.gpx`. One volume.
- **Host:** `citadel` (Dell Optiplex 3020 SFF), rootful Podman with a Quadlet unit, matching the existing pattern.
- **Access:** Tailscale only, plus LAN. Not exposed to the public internet. This is what lets the auth model stay simple.
- **Config:** environment variables. Matching thresholds, progression rule parameters, data path, listen port, import bearer token.

### Backup — needs a real answer

`citadel` runs Nextcloud, so "back up to Nextcloud" would put the backup on the same machine as the data. That is not a backup.

The `/data` volume is the entire application state. It needs a scheduled copy to a **different machine** — the-keeper, or offsite — and the restore path has to be exercised at least once. An untested backup isn't one.

---

## 9. Build order

Both kinds ship in v1, but not in one go. Each stage works before the next begins.

1. **Core model and CLI.** Protocols, sessions, both result tables. GPX parse and derive. `rederive`. No web layer. Get metric definitions right in isolation with real files as fixtures.
2. **Web: workout logging.** Define a workout, log a session, rate it, see history. Workouts first because they need no GPX pipeline, so this is the shortest path to a running app you can use.
3. **Web: walk import.** Upload, derive, rate. Token endpoint plus a working iOS Shortcut. Routes assigned manually for now.
4. **Route matching.** Heuristic scoring and the candidate UI.
5. **Questions view.** Shared per-protocol trends, effort divergence, consistency.
6. **Progression engine.** Last, and not negotiable: it needs history to be worth anything, and with no data it can only suggest repeating yourself. Build it once stages 2–3 have produced a few months of real sessions.

### Deferred, explicitly

- **Local LLM weekly summary.** Qwen on the-keeper (RX 6600), reached over the LAN from citadel; the app never depends on it being up. Worthless until months of real data exist. It should read *derived summaries plus notes*, never raw trackpoints or per-rep results.
- **Apple Health backfill.** One-off export to GPX to seed history. Idempotent import makes this safe to do at any point.
- **Exercise taxonomy.** `exercise` is free text in v1. A controlled vocabulary only pays off if you later want cross-workout volume analysis; don't build it speculatively.
- **Live in-session logging.** Own product. Not this one.

---

## 10. Acceptance criteria for v1

- Logging a completed workout — pick, enter result, rate — takes under 30 seconds on an iPhone.
- Importing a GPX via the share-sheet Shortcut, matching it to a route, and rating it takes under 30 seconds.
- The import endpoint accepts a token-authenticated `multipart/form-data` POST with no browser session.
- Re-importing the same GPX changes nothing.
- `rederive` recomputes all walk metrics with zero loss of effort ratings or notes.
- Scaled and incomplete sessions never drive a progression suggestion, and are visibly marked in history.
- Every progression suggestion displays the rules that produced it.
- Destroying the container and recreating it from the Quadlet unit loses no data.
- A restore from backup onto a different machine has been performed at least once and worked.
- For any protocol with 5+ sessions, the app shows whether the latest was better or worse than typical, and whether that matches how it felt.
