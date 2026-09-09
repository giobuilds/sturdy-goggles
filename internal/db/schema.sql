-- Fit Log schema, stage 1. All timestamps are RFC 3339 UTC text.
-- Principle: store the raw input, derive everything else, never lose
-- subjective data (effort, note).

CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER NOT NULL
);

-- A protocol is a named, repeatable thing whose sessions are compared over
-- time. kind=route is a walk or ride over named ground; kind=workout is a
-- named set of prescribed work.
CREATE TABLE IF NOT EXISTS protocols (
    id          INTEGER PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('route','workout')),
    mode        TEXT CHECK (mode IN ('walk','ride','run')),   -- route only
    name        TEXT NOT NULL UNIQUE,
    notes       TEXT NOT NULL DEFAULT '',
    archived    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);

-- Prescribed work for a workout protocol. Routes have no rows here.
CREATE TABLE IF NOT EXISTS workout_items (
    id           INTEGER PRIMARY KEY,
    protocol_id  INTEGER NOT NULL REFERENCES protocols(id),
    position     INTEGER NOT NULL,
    exercise_id  INTEGER REFERENCES exercises(id),
    label        TEXT NOT NULL,          -- free text when exercise_id is null
    target_reps  INTEGER,
    target_secs  INTEGER,
    UNIQUE (protocol_id, position)
);

-- Vocabulary the Coach composes from and the validator reasons about.
CREATE TABLE IF NOT EXISTS exercises (
    id             INTEGER PRIMARY KEY,
    slug           TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    discipline     TEXT NOT NULL CHECK (discipline IN ('pilates','calisthenics','hiit','mobility')),
    difficulty     INTEGER NOT NULL CHECK (difficulty BETWEEN 1 AND 5),
    equipment      TEXT NOT NULL DEFAULT 'none',
    unit           TEXT NOT NULL CHECK (unit IN ('reps','secs','metres')),
    regression_of  INTEGER REFERENCES exercises(id),
    source         TEXT NOT NULL DEFAULT '',
    notes          TEXT NOT NULL DEFAULT ''
);

-- 'muscle:core', 'avoid:wrist', 'impact:high' ...
CREATE TABLE IF NOT EXISTS exercise_tags (
    exercise_id  INTEGER NOT NULL REFERENCES exercises(id) ON DELETE CASCADE,
    tag          TEXT NOT NULL,
    PRIMARY KEY (exercise_id, tag)
);

-- Optional media; the app is complete without any rows. Licence and
-- attribution are recorded per file and rendered in-app and in NOTICE.
CREATE TABLE IF NOT EXISTS exercise_media (
    id           INTEGER PRIMARY KEY,
    exercise_id  INTEGER NOT NULL REFERENCES exercises(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('svg','image','video','external_link')),
    path_or_url  TEXT NOT NULL,
    licence      TEXT NOT NULL,
    attribution  TEXT NOT NULL DEFAULT '',
    source_url   TEXT NOT NULL DEFAULT ''
);

-- One performed piece of training. Subjective columns (effort, note) are
-- never touched by rederive.
CREATE TABLE IF NOT EXISTS sessions (
    id           INTEGER PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN ('route','workout','activity')),
    mode         TEXT CHECK (mode IN ('walk','ride','run')),   -- route only
    protocol_id  INTEGER REFERENCES protocols(id),             -- null: one-off
    started_at   TEXT NOT NULL,
    effort       INTEGER CHECK (effort BETWEEN 1 AND 5),       -- null until rated
    note         TEXT NOT NULL DEFAULT '',
    completed    INTEGER NOT NULL DEFAULT 1,                   -- 0 = stopped early
    scaled       INTEGER NOT NULL DEFAULT 0,                   -- 1 = modified from prescription
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_protocol ON sessions(protocol_id, started_at);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at);

-- Route sessions only. Every numeric column is derived from the stored source
-- file and re-derivable; source_hash is the sha256 and the filename.
CREATE TABLE IF NOT EXISTS route_metrics (
    session_id      INTEGER PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    source_kind     TEXT NOT NULL CHECK (source_kind IN ('gpx','fit')),
    source_hash     TEXT NOT NULL UNIQUE,
    points          INTEGER NOT NULL,
    distance_m      REAL NOT NULL,
    elapsed_s       INTEGER NOT NULL,
    moving_s        INTEGER NOT NULL,
    ascent_m        REAL NOT NULL,
    start_lat       REAL NOT NULL,
    start_lon       REAL NOT NULL,
    end_lat         REAL NOT NULL,
    end_lon         REAL NOT NULL,
    avg_hr          INTEGER NOT NULL DEFAULT 0,
    max_hr          INTEGER NOT NULL DEFAULT 0,
    derived_at      TEXT NOT NULL,
    derive_version  INTEGER NOT NULL
);

-- Workout sessions only. User-entered; nothing to re-derive.
CREATE TABLE IF NOT EXISTS workout_results (
    session_id   INTEGER PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    total_secs   INTEGER,
    rounds       INTEGER
);

-- Per-exercise actuals for a workout session.
CREATE TABLE IF NOT EXISTS session_exercises (
    id            INTEGER PRIMARY KEY,
    session_id    INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    block         INTEGER NOT NULL DEFAULT 1,
    position      INTEGER NOT NULL,
    exercise_id   INTEGER NOT NULL REFERENCES exercises(id),
    target_value  INTEGER,
    actual_value  INTEGER,
    UNIQUE (session_id, block, position)
);
