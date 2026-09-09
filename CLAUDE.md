# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## State of the repository

Stages 1 and 2 of the build order exist: the core data model, GPX and FIT import, an exercise seed
with five starter workouts, a CLI, and the server-rendered PWA (home with consistency view, session
player with offline save, rating, history). No auth, no import endpoint, no Coach yet. Design lives in two documents for **Fit Log**, a
single-user, open-source personal training system:

- `docs/fitlog-design-brief.md`: the v2 spec (walks + workouts, self-hosted on citadel). Its data-model
  principles and progression rules are still in force. Its non-goals are superseded.
- `docs/fitlog-v3-brief.md`: the current spec. Adds an exercise library, an LLM Coach bounded by a rules
  validator, Apple Watch biometrics via an iOS Shortcuts bridge, daily readiness, and weight-trend
  nutrition targets. Open questions are marked [OPEN] in the file.

Read both before designing anything new. The owner plans through discussion first, so treat the briefs
as the baseline and the owner's conversation as the authority. Native iOS is off the table: the owner's
2017 MacBook is capped at macOS 13, which caps Xcode at 15.2 and cannot target their iOS 26 phone.

The Coach uses the Anthropic API by default. The repo is public: the key is read only from the
`FITLOG_ANTHROPIC_API_KEY` environment variable, never from a config file, and `.env` is gitignored.

## Commands

System Go (Fedora package, 1.26) with `GOTOOLCHAIN=local`; keep `go.mod` at `go 1.26` so it builds.

```
go build -o fitlog ./cmd/fitlog      # single static binary
go test ./...                        # all tests
go test ./internal/track -run TestAscentHysteresis   # one test
go vet ./... && gofmt -l .           # no linter beyond these yet
```

Run against a scratch data directory rather than `./data`:

```
FITLOG_DATA=/tmp/fl ./fitlog init            # creates db + seeds exercises
FITLOG_DATA=/tmp/fl ./fitlog import fixtures/*.gpx fixtures/*.fit
FITLOG_DATA=/tmp/fl ./fitlog session list
FITLOG_DATA=/tmp/fl ./fitlog rederive        # recompute route metrics from stored files
FITLOG_DATA=/tmp/fl FITLOG_LISTEN=127.0.0.1:8080 ./fitlog serve   # web app
```

`fitlog help` lists every command. Effort ratings and notes survive `rederive` by design; a test
enforces it.

## Fixtures and privacy

`fixtures/` is gitignored because the real recordings carry the owner's home GPS coordinates. Tests
that need real files (`fitfile`, `importer.TestImportRealFixtures`) skip when the directory is absent.
`testdata/short-walk.gpx` is a coordinate-shifted copy safe for the public repo. Never copy anything
from `fixtures/` into a tracked path without shifting coordinates.

## Code layout

- `internal/track`: source-independent `Track` and `Derive` (distance, moving time, ascent with
  hysteresis, HR). The only place metric definitions live. Bump `DeriveVersion` when they change.
- `internal/gpx`, `internal/fitfile`: parsers producing a `Track`. FIT also returns the head unit's own
  totals for comparison; they are never stored.
- `internal/db`: embedded `schema.sql`, applied idempotently. `route_metrics` is what the v2 brief
  called `walk_metrics`; it now also serves rides and carries HR.
- `internal/store`: all SQL. `internal/importer`: hashes a file, copies it under
  `<data>/<gpx|fit>/<sha256>.<ext>`, creates the session and metrics, and implements `Rederive`.
- `internal/seed/exercises.json`: hand-authored, CC0. Regressions are referenced by slug and must
  appear earlier in the file than the exercise that points at them. `starters.json` defines the shipped
  beginner workouts; they are created once by name and never overwritten, so user edits survive.
- `internal/web`: `server.go` holds routes, view models and handlers; `templates/` are html/template
  pages sharing `layout.html`; `static/` is embedded. `player.js` is the offline session player: it
  POSTs JSON to `/api/sessions` and queues in localStorage when that fails; `app.js` flushes the queue
  on every page load. `sw.js` caches the shell; bump its `VERSION` when static files change.
- Time zone: sessions are stored UTC; weeks start on local Monday in `FITLOG_TZ` (default system).

## Intended stack (v2 §8, v3 §10)

- Go, single static binary. SQLite via `modernc.org/sqlite` (pure Go, **no cgo**). GPX parsed with `encoding/xml`.
- Server-rendered HTML installed as a PWA (manifest + service worker). One vanilla-JS module for the offline session player. No SPA framework, no bundler. Charts as server-rendered SVG.
- Coach LLM behind a provider interface (Anthropic API or an OpenAI-compatible local endpoint). The app must run with no provider configured.
- Storage in one volume: `/data/fitlog.db` and `/data/gpx/<sha256>.gpx`.
- Deployed on host `citadel` under rootful Podman with a Quadlet unit; reachable on LAN and Tailscale only. Never exposed publicly.
- All configuration via environment variables: route-matching thresholds, progression rule parameters, data path, listen port, import bearer token. Thresholds and rule numbers must live in config, not as scattered literals.

## Architecture: the load-bearing ideas

**Protocol → Sessions.** A *protocol* is a named repeatable thing (kind `route` for walks, kind `workout`
for prescribed work). A *session* is one performance of a protocol on a date. History, comparison,
consistency, and progression all operate on "protocol → sessions over time" and are indifferent to kind.
The two kinds differ in exactly one place: how results arrive (walk metrics derived from a GPX upload;
workout results typed in by hand). Resist any change that makes the kinds diverge beyond that.

**Store raw input, derive everything else.** Uploaded GPX files are immutable, kept on disk, named by
sha256 content hash. All walk metrics are derived values carrying `derive_version` and a pointer to the
source file, and a `rederive` command recomputes every walk session from stored files. Subjective data
(`effort` 1–5, `note`) is never derivable and must survive re-derive untouched. `gpx_hash UNIQUE` is what
makes walk import idempotent. Schema sketch is in brief §3.

**`scaled` and `completed` gate comparability.** A session that was modified or stopped early is shown
in history, visibly marked, but never feeds a trend or a progression suggestion.

**Route matching is heuristics + human confirmation (§4).** Score existing routes by haversine distance
between start points, between end points, and ratio of total distance to the route's median. Present
the best candidate pre-selected; the user always confirms. Do not attempt trace-to-trace matching
(Fréchet, Hausdorff, map-matching); it is explicitly out of scope.

**The import endpoint is designed for an iOS Shortcut (§5).** It must accept a bare
`multipart/form-data` POST authenticated by a bearer token header, with no session cookie or CSRF token.
Build it that way from the start; retrofitting around cookie auth is the known trap.

**Progression engine boundaries (§6, read before touching it).** `suggest(protocol, history) ->
(target, reasoning[])` is deterministic and rules-based, nothing learned or statistical beyond medians,
and always returns the reasoning the UI displays. It proposes a target for a workout the user already
chose. It must not plan what to train, when to rest, or how to periodise; that boundary is deliberate
because getting it wrong has physical consequences. The rules are tuned so failure is *too cautious*,
never *too aggressive* (hold after effort 5, back off on repeated struggle, reset after a >3-week
layoff, cap at 10% above best comparable). Preserve that asymmetry when editing rules.

**Language rule for all generated text.** Describe what the data shows, never evaluate the person.
"4 minutes slower than your median, effort rated lower", not "you're slowing down" or "good job". No
streak language, no scolding, no comparison against anyone but the user's own history.

**Entry flows are the product.** Logging a workout or importing a walk, then rating it, must each take
under 30 seconds on an iPhone; the shared rate step under 20 seconds. Mobile-first, thumb-sized
controls. Sessions may be saved unrated and rated later.

## Non-goals (do not build)

No social features or multi-user accounts, no in-app GPS recording, no calorie/macro/weight tracking,
no generated training programmes, no public internet exposure. Explicitly deferred: local LLM weekly
summary, Apple Health backfill, exercise taxonomy (`exercise` is free text), live in-session logging.

## Build order (§9)

Each stage should work before the next begins:

1. Core model and CLI: protocols, sessions, both result tables, GPX parse/derive, `rederive`. Use real GPX files as test fixtures. No web layer.
2. Web: workout logging (shortest path to a usable app).
3. Web: walk import, token endpoint, iOS Shortcut. Routes assigned manually.
4. Route matching.
5. Questions view (per-protocol trends, effort vs result divergence, weekly consistency).
6. Progression engine, last, once real history exists.

Acceptance criteria for v1 are listed in brief §10.
