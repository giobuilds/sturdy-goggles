# Fit Log — v3 Brief (draft for discussion)

**Status:** draft, 2026-09-09. Supersedes v2 in scope. v2's data-model principles are carried forward; its non-goals are revised below.
**Implementation:** stage 1 built 2026-09-09 (core model, GPX + FIT import, seed, CLI); stage 2 built 2026-09-09 (PWA: home with consistency view, five starter workouts, offline session player, rating, history). Note the v2 `walk_metrics` table is implemented as `route_metrics`, serving walks and rides, with `source_kind`, `avg_hr` and `max_hr` added.
**Author:** Gio, with Claude
**Target:** self-hosted PWA on `citadel`, reached over Tailscale and LAN; open source on GitHub

Open questions are marked **[OPEN]** inline and collected in §12.

---

## 1. Decisions so far

| Question | Decision | Consequence |
|---|---|---|
| Who is it for | Gio, personally. Open source, so others may self-host it. | Single-user model. No accounts. Clean config and setup docs so a stranger can run it. Safety framing matters because strangers may use it. |
| Platform | Web app installed as a PWA. Native iOS is blocked (see §3). | Wearable data arrives through a Shortcuts bridge, not HealthKit directly. No live heart rate on screen during a session. |
| Coach autonomy | Composes sessions freely from an exercise library, within enforced constraints. Also proposes targets for named benchmark workouts. | LLM is the composer; deterministic rules are the validator and adapter. Nothing the LLM says reaches the plan without passing validation. |
| Nutrition | Weight trend and calorie/protein targets in v1. Recipes later. | No meal logging, no food database. Weight comes from Apple Health via the bridge. |
| Hosting | `citadel`, rootful Podman, Quadlet. | Unchanged from v2. |

---

## 2. Purpose (revised)

### Starting from zero

Gio does not currently train. The app's first job is to get someone from nothing to a habit and keep them there, not to optimise an athlete. This reorders priorities:

- **Consistency is the primary metric.** Sessions per week beats every performance number until months of history exist. The consistency view (v2 §7) moves up the build order and onto the home screen.
- **There are no benchmark workouts to start with.** The v2 model assumed named workouts the user already does. Instead the app ships a small hand-authored starter set of beginner sessions, composed from the exercise library, 10–20 minutes, mat only, difficulty 1–2. These become the first protocols. The Coach (stage 8) inherits and adapts them later; nobody has to invent a workout on day one.
- **Beginner defaults are conservative by construction.** Two sessions a week, not five. Difficulty 1 everywhere. Progression rules unchanged, but the ceiling and increment caps are already the cautious end.
- **Walking and riding count as training.** They are the activities that already happen. A walk logged is a session logged, and it feeds consistency the same as a workout does.
- **No shame, ever.** The language rule (§6.4, v2 §7) matters more for a beginner than for anyone. A missed fortnight is shown as a gap in the chart, never described in words.

Placeholder names in the v2 brief ("Aphrodite", from Freeletics) are not to be used. Starter sessions get plain descriptive names.

**Equipment on hand:** mat, one elastic band, two adjustable dumbbells (plates 2×0.5, 2×1.25, 2×2.5 kg per dumbbell, so up to about 8.5 kg a hand). No pull-up bar. The starter set and the validator's equipment filter use exactly this list; bar exercises stay in the library but out of any plan until a bar exists.

A personal training system that **answers questions about your own history** and **proposes what to do next**, across walking, Pilates, calisthenics and HIIT, driven by a stated goal and informed by wearable data.

The v2 test still holds: every feature must serve a question like "is this getting easier?", "was that slow or did it just feel slow?", "have I been consistent?", "what should I aim for?". v3 adds two more:

- "Given my goal and how recovered I am, what should today's session be?"
- "Is my body responding the way the plan expects?"

### Non-goals (revised)

- No social features, sharing, leaderboards, or multi-user accounts.
- No live GPS tracking or live heart-rate display in-app. Recording is delegated to the Watch and to GPX exporters.
- No meal logging or food database in v1. (Recipes: deferred, §9.)
- No public internet exposure.
- **The Coach never overrides a hard safety rule.** It may take liberties with exercise selection and structure. It may not exceed volume caps, ignore contraindications, or schedule work when readiness rules say rest.

---

## 3. Platform

### Why not native iOS

Gio's Mac is an Intel MacBook on macOS 13.7.8 (Ventura). The last Xcode that runs on Ventura is 15.2, which cannot deploy to an iPhone on iOS 26. Xcode 26 needs macOS 15.6 or later. Every cross-platform toolchain (Flutter, React Native, Kotlin Multiplatform) still needs Xcode for the iOS build, so they are blocked too.

The MacBook is a 2017 model, capped at Ventura. Native iOS is off the table for as long as this is the development machine. Not revisited in v3.

### What we build instead

- **PWA.** Server-rendered HTML with a web manifest and a service worker. Installable to the iPhone home screen, full-screen, offline-capable for the session player. iOS supports web push for installed PWAs if we ever want reminders.
- **Session player** is the one place that needs real client-side JS: timers, rest intervals, per-exercise "done / actual reps" entry, and it must keep working if the connection drops mid-session. Sync when back online.
- **Health bridge.** An iOS Shortcut automation, run nightly and on demand, that uses the `Find Health Samples` action to pull the last N days of samples and POSTs them as JSON to a token-authenticated endpoint. Same endpoint pattern as v2's GPX import. Health Auto Export (App Store) is a fallback that does the same with a nicer scheduler.
- **Weight bridge.** The scale is a Fitbit Aria Air (FB203), Bluetooth-only, which syncs to the Fitbit app and not to Apple Health. The app pulls weight logs directly from the Fitbit Web API (OAuth2 with PKCE, refresh token kept under `/data`, nightly pull, idempotent by log id). This is a second small bridge, weight only, and the Aria Air reports only weight and BMI. Fallback: manual entry in the PWA.
- **Ride import.** Cycling is recorded on a Bryton Rider 420 with a Bryton heart-rate strap. The app accepts FIT files, which carry the HR series that GPX from the head unit would lose. Export path: Bryton Active app share sheet (confirmed to offer `.fit`), through the same iOS Shortcut and token endpoint as GPX. FIT parsing in Go via `github.com/tormoder/fit`. A chest strap is more accurate than the wrist, so where a ride has both a Bryton FIT and a Watch record, the FIT wins and the Watch record is linked, not duplicated.
- **Workout recording** happens on the Apple Watch's own Workout app. The user starts "HIIT" / "Pilates" / "Strength" on the wrist, does the session from the PWA, ends it on the wrist. The bridge later imports the workout record with its heart-rate series, and the app matches it to the logged session by time overlap. This is the biometric analogue of route matching: heuristic, with user confirmation.

The Watch is a Series 10 on watchOS 26. It provides resting HR, HRV (SDNN), sleep stages and sleep score, wrist temperature deviation, blood oxygen where enabled, and per-workout heart-rate series. All of these are in scope for readiness. Verified 2026-09-09: `Find Health Samples` exposes "Wrist Temperature" as a type and returns one absolute reading per night in °C (e.g. 36.78 °C at 01:11), not the deviation the Health app displays. The bridge stores the absolute value; the deviation is derived against our own 28-day baseline like every other input.

---

## 4. Core concepts

v2's abstraction survives: a **protocol** is a named repeatable thing whose **sessions** are compared over time. v3 adds the layer above it (goals and plans) and the layer below it (exercises).

- **Exercise.** An atomic movement: "push-up", "hollow hold", "Pilates hundred". Has discipline, muscle groups, difficulty, equipment, a progression and regression chain, and contraindication tags ("shoulder", "lumbar", "wrist"). This is the vocabulary the Coach composes from. Free text is no longer enough, because the validator has to reason about it.
- **Session.** One performed piece of training on a date. Kinds: `route` (walk or ride over named ground, derived from a GPX or FIT file; `mode` distinguishes walk from ride so paces are never compared across modes), `workout` (structured, exercise-based), `activity` (anything imported from the Watch that isn't one of ours, e.g. a swim; counts toward training load, never compared). A session may have a **plan** (what the Coach proposed) and a **result** (what happened). Both are stored. Divergence between them is a signal.
- **Benchmark protocol.** v2's named workout ("Aphrodite") or route ("Canal loop"). Repeated deliberately so results can be compared. Coach-composed sessions are one-offs by default. The user can promote one to a benchmark.
- **Goal profile.** Single row. Primary goal (fat loss, strength, endurance, mobility, general), weekly session budget, available equipment, disciplines wanted, injuries and constraints from onboarding.
- **Plan.** The Coach's rolling proposal for the next 7 days. Regenerated on demand and whenever readiness or the goal changes. Each planned session records the reasoning that produced it.
- **Readiness.** A daily score derived from resting HR, HRV, sleep, and recent training load, each compared to the user's own rolling baseline. Drives adaptation. Shown with its inputs, never as a bare number.
- **Biometric sample.** Raw imported value with timestamp, source, and type. Stored as received. Everything else (baselines, readiness) is derived and re-derivable, exactly as GPX metrics are in v2.

---

## 5. Data model sketch (SQLite, additions to v2)

Principle unchanged: **store the raw input, derive everything else, never lose subjective data.**

```sql
-- Vocabulary the Coach composes from
CREATE TABLE exercises (
    id             INTEGER PRIMARY KEY,
    slug           TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    discipline     TEXT NOT NULL,   -- pilates | calisthenics | hiit | mobility
    difficulty     INTEGER NOT NULL, -- 1..5
    equipment      TEXT,            -- none | mat | bar | band | ...
    unit           TEXT NOT NULL,   -- reps | secs | metres
    regression_of  INTEGER REFERENCES exercises(id),
    source         TEXT,            -- where the definition came from
    notes          TEXT
);
CREATE TABLE exercise_media (      -- optional; app is complete without any rows
    id             INTEGER PRIMARY KEY,
    exercise_id    INTEGER NOT NULL REFERENCES exercises(id),
    kind           TEXT NOT NULL,   -- svg | image | video | external_link
    path_or_url    TEXT NOT NULL,   -- under /data/media for local files
    licence        TEXT NOT NULL,   -- CC0 | CC-BY-4.0 | CC-BY-SA-4.0 | own | external
    attribution    TEXT,            -- rendered in-app and in NOTICE
    source_url     TEXT
);
CREATE TABLE exercise_tags (       -- muscle groups AND contraindications
    exercise_id    INTEGER NOT NULL REFERENCES exercises(id),
    tag            TEXT NOT NULL,   -- 'muscle:core', 'avoid:wrist', ...
    PRIMARY KEY (exercise_id, tag)
);

-- Single-user profile and goal
CREATE TABLE profile (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    goal           TEXT NOT NULL,
    sessions_per_week INTEGER NOT NULL,
    disciplines    TEXT NOT NULL,   -- JSON array
    equipment      TEXT NOT NULL,   -- JSON array
    constraints    TEXT NOT NULL,   -- JSON array of 'avoid:*' tags
    birth_year     INTEGER,
    height_cm      REAL,
    sex_for_estimates TEXT,         -- only used by TDEE formula
    hr_max_override INTEGER,        -- if measured; else 220-age
    updated_at     TEXT NOT NULL
);

-- What the Coach proposed (plan) and what was done (result) live on the same session
CREATE TABLE sessions (
    id             INTEGER PRIMARY KEY,
    kind           TEXT NOT NULL CHECK (kind IN ('route','workout','activity')),
    mode           TEXT,            -- route only: walk | ride
    protocol_id    INTEGER REFERENCES protocols(id),   -- set for benchmarks
    load           REAL,            -- derived training load (§7), re-derivable
    planned_at     TEXT,            -- date the plan targeted, if planned
    started_at     TEXT,            -- null until performed
    structure      TEXT,            -- circuit | amrap | emom | flow | intervals | straight
    plan_json      TEXT,            -- Coach's proposed blocks, validated
    plan_reasoning TEXT,            -- human-readable, always shown
    coach_run_id   INTEGER REFERENCES coach_runs(id),
    effort         INTEGER,         -- 1..5, subjective, never overwritten
    note           TEXT,
    completed      INTEGER NOT NULL DEFAULT 1,
    scaled         INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL
);
CREATE TABLE session_exercises (   -- per-exercise actuals for workouts
    id             INTEGER PRIMARY KEY,
    session_id     INTEGER NOT NULL REFERENCES sessions(id),
    block          INTEGER NOT NULL,
    position       INTEGER NOT NULL,
    exercise_id    INTEGER NOT NULL REFERENCES exercises(id),
    target_value   INTEGER,
    actual_value   INTEGER,
    UNIQUE (session_id, block, position)
);

-- Raw wearable data, as received
CREATE TABLE biometrics (
    id             INTEGER PRIMARY KEY,
    type           TEXT NOT NULL,   -- resting_hr | hrv_sdnn | sleep | steps | weight | hr | ...
    ts             TEXT NOT NULL,
    end_ts         TEXT,            -- for interval samples (sleep, workouts)
    value          REAL NOT NULL,
    unit           TEXT NOT NULL,
    source         TEXT NOT NULL,   -- 'apple_watch', 'scale', 'manual'
    import_hash    TEXT NOT NULL,   -- dedupe key; idempotent import
    UNIQUE (import_hash)
);
-- Imported device workouts (Watch, Bryton FIT), linked to sessions by time overlap after confirmation
CREATE TABLE device_workouts (
    id             INTEGER PRIMARY KEY,
    session_id     INTEGER REFERENCES sessions(id),
    source         TEXT NOT NULL,   -- apple_watch | bryton_fit
    activity_type  TEXT NOT NULL,
    started_at     TEXT NOT NULL,
    ended_at       TEXT NOT NULL,
    avg_hr         REAL, max_hr REAL, active_kcal REAL,
    hr_series_path TEXT,            -- raw series kept on disk, not in rows
    import_hash    TEXT NOT NULL UNIQUE
);

-- Derived daily readiness, re-derivable
CREATE TABLE readiness (
    day            TEXT PRIMARY KEY,
    score          REAL,            -- 0..100
    inputs_json    TEXT NOT NULL,   -- each input, its baseline, its z-score
    derive_version INTEGER NOT NULL
);

-- Every Coach invocation: what it saw, what it said, whether it passed
CREATE TABLE coach_runs (
    id             INTEGER PRIMARY KEY,
    ran_at         TEXT NOT NULL,
    purpose        TEXT NOT NULL,   -- plan_week | suggest_target | weekly_review | recipe
    provider       TEXT NOT NULL,
    model          TEXT NOT NULL,
    input_summary  TEXT NOT NULL,   -- the summary the model was given, verbatim
    raw_output     TEXT NOT NULL,
    validation     TEXT NOT NULL,   -- JSON: passed, violations, adjustments applied
    accepted       INTEGER NOT NULL DEFAULT 0
);
```

Storing `input_summary`, `raw_output` and `validation` on every run is what makes the Coach debuggable. When a plan looks wrong, you read what it was told and what the validator changed.

---

## 6. The Coach

Three parts. Only the first is an LLM.

### 6.1 Composer (LLM)

Given a **Coach brief** (a compact text summary, never raw rows) it produces structured JSON: a week of sessions, each with a structure, blocks, exercises by slug, targets, and a reasoning paragraph. It is free to choose disciplines, structures and exercises within the goal profile. That is the flexibility asked for: no fixed template library to maintain.

The Coach brief contains: goal profile, last 4 weeks of sessions as one line each (date, structure, main exercises, effort, completion), current readiness and its trend, benchmark results and medians, the exercise slugs it is allowed to use with their tags, and the constraints below stated in plain words.

Provider is pluggable behind one interface. **Anthropic API is the default.** An OpenAI-compatible endpoint is the second implementation, for a local model later (Gio's Lowerbeam project once it is mature; Qwen on the-keeper as a stopgap). The app runs without any provider configured; it just has no Coach.

Because the repository is public, the key never touches the repo:

- Read from `FITLOG_ANTHROPIC_API_KEY` only. No config-file field for it, so it cannot be committed by accident.
- Commit a `.env.example` with the variable name and an empty value. `.env` is gitignored from the first commit.
- On `citadel`, pass it to the Quadlet unit via `EnvironmentFile=` pointing at a root-only file under `/etc/fitlog/`, or via a Podman secret. Never in the `.container` file itself.
- The Coach brief sent to the API contains summaries only, never notes verbatim unless the user opts in. Documented in the README so a self-hoster knows what leaves their machine.
- A pre-commit secret scan (gitleaks) in CI as a backstop.

### 6.2 Validator (rules)

Rejects or adjusts the composer's output. Every violation is recorded and shown. Constraints, all in config:

1. Only exercises from the library. Unknown slugs are rejected.
2. No exercise whose `avoid:*` tags intersect the profile constraints.
3. Weekly volume per muscle-group tag within a cap derived from recent history (no more than +10% per week over the 4-week average, the classic conservative rule).
4. No more than two consecutive high-intensity days. At least one full rest day per week.
5. Difficulty of any exercise no more than one step above the highest difficulty completed as prescribed in the last 4 weeks.
6. If readiness is below the low threshold, the day's session is downgraded to mobility or rest regardless of what was composed.
7. Session duration within the profile's budget.
8. Targets for benchmark protocols must obey the v2 progression rules (§6 of the v2 brief), which remain in force unchanged.

If validation fails, the validator asks the composer once for a revision with the violations listed. If it fails again, the app falls back to repeating the last valid week and says so.

### 6.3 Adapter (rules)

Runs daily without the LLM. Adjusts today's planned session in place from the latest readiness and from yesterday's result: reduce intensity on low readiness, swap to a regression when the same exercise was incomplete twice, insert rest after two consecutive effort-5 sessions. Same asymmetry as v2: the failure mode must be *too cautious*.

### 6.4 Presentation

Every plan and every adjustment shows its reasoning and which rule, if any, changed it. Suggestions are editable defaults. No instructions, no scolding, no streak language. Language rule from v2 §7 applies to all Coach output, and the composer prompt states it explicitly.

---

## 7. Biometrics and readiness

Imported nightly via the bridge: resting heart rate, HRV (SDNN, which is what Apple exposes), sleep intervals, stages and score, step count, body mass, wrist temperature deviation, blood oxygen, and workouts with heart-rate series. All available on the Series 10.

**Readiness** is computed per day from each input's deviation from a 28-day rolling personal baseline. Rough first draft:

- Resting HR above baseline: negative, weighted heavily. This is the most reliable single overtraining and illness signal.
- HRV below baseline: negative, moderate weight. Noisy; smooth over 3 days.
- Sleep duration below baseline: negative, moderate weight. Sleep score from watchOS 26 can be shown alongside but is not an input, since its formula is Apple's and opaque.
- Wrist temperature above baseline: negative, small weight on its own, but combined with elevated resting HR it triggers the anomaly hold (§9) as a likely illness signal.
- Acute:chronic training load ratio (7-day / 28-day session load) above 1.3: negative.
- Result mapped to 0–100 with three bands: go, easy, rest.

**Training load** per session, stored on the row and re-derivable:

- With a heart-rate series (Watch or Bryton strap): Banister TRIMP, the published exponential formula over time in each HR zone. This makes a hard ride, a HIIT circuit and a long walk comparable on one scale.
- Without one: session RPE, Foster's published method, which is simply duration in minutes times effort. Our 1–5 effort scale maps onto Foster's 0–10.
- Watch `activity` sessions count in full. This is why they are imported at all.
- Acute:chronic ratio uses the exponentially weighted version (7-day and 28-day time constants) rather than rolling sums, because the rolling-sum form is known to lag.

**Formula decision: hybrid.** Session load and the acute:chronic ratio use the published methods above, because they exist, are documented, and their behaviour is understood. The combination into one readiness score is home-grown: each input's deviation from its own 28-day baseline, weighted, summed, banded. No published formula fits Apple's inputs exactly. HRV4Training and similar use morning rMSSD readings; Apple provides opportunistic SDNN samples through the night, which are noisier and on a different scale. Oura and Whoop scores are proprietary. So the honest choice is published parts, transparent glue, every weight in config, every input shown. Apple's own Vitals app on watchOS flags overnight outliers using a similar baseline idea, but it does not export its flags, so it serves as a sanity check for ours, not a source.

Heart-rate zones use `hr_max_override` if measured, else 220 minus age, and the app never plans work above zone 4 for a fat-loss or general goal. Watch heart-rate series are used after the fact for "time in zone" per session and for spotting sessions where average HR was far above the plan's intent.

---

## 8. Nutrition (v1 scope)

- **Weight trend.** Body mass from the bridge (smart scale to Health) or manual entry. Displayed as an exponentially smoothed trend line over daily points, because daily weight is noise.
- **Targets.** Estimated maintenance from Mifflin-St Jeor plus an activity factor from actual logged sessions. A daily calorie budget and a protein target follow from the goal. The budget self-corrects: if the trend moves faster or slower than the goal's rate, the budget shifts by a small capped step. No intake logging is needed for this; the scale is the feedback.
- **Guardrails.** Minimum budget floor. Maximum loss rate of 0.5–1% of body mass per week. If the trend exceeds it, the app says so and raises the budget.

**Deferred: recipes.** A Coach purpose (`recipe`) that generates meals hitting the day's remaining protein and calories from a pantry list. Sits naturally on the Coach once the targets exist. Not in v1.

---

## 9. Safety

Because the code is public, assume a stranger with a heart condition might run it.

- **Onboarding screen** based on PAR-Q+ (the standard pre-exercise screening questionnaire). A "yes" to any item shows a "see a doctor first" notice and sets conservative caps. It does not block use; this is a personal tool, not a gatekeeper.
- **Injury and constraint tags** in the profile map directly to exercise `avoid:*` tags. The validator enforces them.
- **Anomaly hold.** Resting HR more than a configured margin above baseline for two days, or a user-reported illness, forces readiness to "rest" until it clears.
- **Hard caps** in the validator are not overridable from the Coach prompt. Only the config file changes them.
- **Disclaimer** in the README and on the onboarding screen: general-wellness tool, not medical advice, not a medical device.
- **Data stays local.** Only the Coach brief summary leaves the machine, and only if a cloud provider is configured. The README says exactly what is sent.

---

## 10. Stack

- **Backend:** Go, single static binary, `modernc.org/sqlite`. Same as v2.
- **Frontend:** server-rendered HTML templates. A PWA manifest and service worker. One vanilla-JS module for the session player with local persistence and sync. No framework, no bundler.
- **Charts:** server-rendered SVG.
- **Coach provider:** interface with two implementations, Anthropic Messages API and OpenAI-compatible HTTP. Structured JSON output, schema-validated before the rules validator sees it.
- **Exercise library seed:** bootstrap from `free-exercise-db` (JSON, ~800 exercises, text data released as public domain; its images have unclear provenance, so vendor the JSON only, never the images). `wger`'s data is CC-BY-SA 3.0, which is usable with attribution but would put share-alike data inside an MIT repo, so treat it as a reference, not a vendored source. Both are strength-and-gym heavy; Pilates and most calisthenics progressions will be hand-authored in a separate seed file with its own attribution.
- **Exercise media.** Wanted, but licence-clean and optional. Three tiers, each recorded per file in `exercise_media` with its licence and attribution, all rendered in-app and in a `NOTICE` file:
  1. **Line-art SVG** for every exercise, committed to the repo under CC0 in `assets/exercises/`. Small, theme-aware, offline. Generated with an image model or drawn once, in one consistent style. Output of an image model carries no third-party copyright, so this is the clean default.
  2. **Own video clips.** 5–10 second loops shot on the iPhone, stored under `/data/media/`, never in git (size). Distributed as an optional media pack, or simply absent on other people's installs. The session player uses them when present and the SVG otherwise.
  3. **Third-party stills** from Wikimedia Commons only, restricted to CC0, CC-BY and CC-BY-SA, fetched at setup by a `fitlog media fetch` command from their source URLs with attribution, not vendored. Fetching at setup keeps share-alike material out of the MIT repo. `free-exercise-db`'s images are excluded until their provenance is confirmed.
  4. **External links.** An `external_link` row can point at a YouTube video for an exercise with no local media. Opened in the browser, never embedded, so the offline player stays offline and no third-party player lands in the PWA.
- **Storage:** `/data/fitlog.db`, `/data/gpx/`, `/data/fit/`, `/data/media/`, `/data/imports/` for raw bridge payloads. One volume.
- **Deploy:** Podman Quadlet on `citadel`. Backup to a different machine, restore tested, as v2 demanded.
- **Config:** environment variables for secrets, paths and ports; one TOML file for rule parameters, since there are now too many for env vars alone. Secrets are env-only (§6.1).

---

## 11. Build order

Each stage is usable before the next starts. v2's stages 1–3 are the foundation and are kept.

1. **Core model and CLI.** Protocols, sessions, exercises with seed data, GPX derive, `rederive`. Tests use real GPX and real Health exports as fixtures.
2. **Web: workout logging and session player.** Define, run, log, rate. PWA install. This is the daily-use loop and must be fast on the phone.
3. **Web: walk and ride import, route matching.** v2 stages 3–4, plus FIT parsing and `mode`. Route matching is mode-aware.
4. **Health and weight bridges.** Shortcut, endpoint, idempotent import, device-workout-to-session matching with confirmation. Fitbit API pull for weight. Weight trend view arrives here.
5. **Readiness and the adapter.** Baselines, daily score, rule-based in-place adjustments. No LLM yet. This alone already makes the app adaptive.
6. **Questions view.** Per-protocol trends, effort versus result, time in zone, consistency.
7. **Benchmark progression.** v2's §6 engine, unchanged.
8. **Coach composer.** Provider interface, Coach brief, validator, weekly plan, weekly review. Last, because it needs history, an exercise library, readiness, and the validator all in place to be worth anything.
9. **Nutrition targets.** Maintenance estimate, budget, self-correction.

Deferred: recipes, native iOS app if the Mac situation changes, exercise videos or images, Android Health Connect bridge (only if a contributor wants it).

---

## 12. Open questions

Resolved 2026-09-09: MacBook is 2017 (native out for good); Watch is Series 10; Coach provider is Anthropic API with local via Lowerbeam later.

Resolved 2026-09-09 (later): scale is a Fitbit Aria Air, pulled via Fitbit Web API; readiness is hybrid (§7); Watch activities count toward load; cycling from a Bryton Rider 420 is a first-class `route` with `mode = ride`, imported as FIT; media strategy in §10.

Resolved 2026-09-09 (evening): Bryton Active shares `.fit`; Shortcuts exposes wrist temperature as absolute °C.

1. Whether the Fitbit Web API still issues personal-app credentials without a business account. Google has been folding Fitbit into Google accounts; check at implementation time. Fallback is a Health-sync app on the phone, then the existing Health bridge. Two-minute check on the phone: open Shortcuts, add a `Find Health Samples` action, and see whether "Wrist Temperature" is in the type list. If not, Health Auto Export is the path for that one metric.

Resolved by research: exercise dataset licensing (§10).
