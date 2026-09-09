# Fit Log

A personal training log that answers questions about your own history and proposes what to do next.
Walks and rides from GPX or FIT files, workouts entered by hand, an exercise library, and (later) an
adaptive Coach, wearable readiness, and weight-trend targets. Single user, self-hosted, no accounts.

Design: [`docs/fitlog-v3-brief.md`](docs/fitlog-v3-brief.md). Status: stage 2 (CLI plus the workout web app).

## Build

Requires Go 1.26 or later. Pure Go, no cgo.

```
go build -o fitlog ./cmd/fitlog
go test ./...
```

## Use

```
export FITLOG_DATA=./data          # one directory holds the database and every imported file
./fitlog init                      # create the database, seed the exercise library
./fitlog protocol add --kind route --mode ride --name "Evening loop"
./fitlog import --protocol "Evening loop" ride.fit
./fitlog import walk.gpx
./fitlog workout log --protocol "Aphrodite" --secs 1520 --effort 4
./fitlog session list
./fitlog session rate 3 --effort 3 --note "felt easy"
./fitlog rederive                  # recompute all route metrics; ratings and notes are untouched
```

Imported files are stored immutably under `FITLOG_DATA`, named by SHA-256. Importing the same file
twice changes nothing. All numeric metrics are derived from those files and can be recomputed.

## Web app

```
./fitlog serve                     # http://localhost:8080, set FITLOG_LISTEN to change
```

Open it on your phone, use "Add to Home Screen", and it installs as an app. The session player works
with no connection: a finished session is kept on the phone and synced the next time a page loads.
Five beginner workouts ship with the app (mat, dumbbells, band). There is no login: run it on a
LAN or over Tailscale only.

## Configuration

Environment variables only. See [`.env.example`](.env.example). Secrets never live in a config file.

## Privacy

Everything stays on your machine. The optional Coach (not yet built) sends a summary to the
configured LLM provider and nothing else; the README will document exactly what when it exists.

## Licence

MIT. The exercise library in `internal/seed/exercises.json` is CC0.
