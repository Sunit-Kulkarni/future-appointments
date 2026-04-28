# Future Appointments

A small Encore.go service for booking 30-minute training sessions with
trainers. Three endpoints, real per-trainer availability, real IANA
timezone handling.

Built as a Staff Backend take-home for [Future](https://future.co/).

## Endpoints

| Method | Path                                          | Purpose                                           |
| ------ | --------------------------------------------- | ------------------------------------------------- |
| GET    | `/trainers/:trainer_id/slots`                 | List available 30-min slots in a window           |
| POST   | `/appointments`                               | Book a 30-min appointment                         |
| GET    | `/trainers/:trainer_id/appointments`          | List a trainer's appointments (with user info)    |

### `GET /trainers/:trainer_id/slots`

Query params:
- `starts_at` (RFC3339, required) — window start.
- `ends_at` (RFC3339, required) — window end.
- `timezone` (IANA name, optional) — format response times in this zone;
  defaults to the trainer's stored timezone.

Returns 30-min slots aligned to `:00` / `:30`, falling inside the trainer's
weekday availability blocks, excluding any already-booked time, and
filtering out anything in the past.

### `POST /appointments`

Body:
```json
{
  "trainer_id": 1,
  "user_id": 2,
  "starts_at": "2026-05-04T09:00:00-07:00",
  "ends_at":   "2026-05-04T09:30:00-07:00"
}
```

Validates against the trainer's local-time availability for that weekday,
exact 30-minute duration, `:00` / `:30` boundary, and not-in-past. Wraps
overlap-check + insert in a transaction; returns `AlreadyExists` on
conflict (overlap or unique violation `23505`).

### `GET /trainers/:trainer_id/appointments`

JOINs in trainer name + user name, returns times formatted in the trainer's
local timezone.

## Setup

> **No Dockerfile needed.** The take-home doc mentions a Dockerfile would
> be appreciated; Encore is the "or equivalent" — `encore run` provisions
> Postgres in Docker for you, applies migrations, runs the seed init, and
> exposes a dev dashboard. There's no `docker compose` or hand-written
> Dockerfile to maintain.

### 1. Install prerequisites

```bash
# macOS
brew install encoredev/tap/encore
# Docker Desktop must also be running (Encore provisions Postgres into Docker)
```

For other platforms, see <https://encore.dev/docs/install>. Anything ≥
Encore CLI v1.50 works.

### 2. Clone and run

```bash
git clone <this-repo> future-appointments
cd future-appointments
encore run
```

That's it — no separate `migrate`, `db create`, or seed step. On first
boot Encore:

1. Provisions a Postgres container if one isn't already running.
2. Creates the `appointments` database and applies
   [`appointments/db/migrations/1_create_tables.up.sql`](appointments/db/migrations/1_create_tables.up.sql).
3. Runs the service's `init()`, which loads
   [`fixtures.sql`](appointments/db/fixtures.sql) (3 trainers, 10 users,
   M–F 08:00–17:00 availability) and the historical appointments from
   [`appointments/appointments.json`](appointments/appointments.json).
   Seeding is gated to `encore.CloudLocal`, so it never runs in staging
   or production.

### 3. Verify

Two endpoints to sanity-check:

```bash
# All three trainers, with seed users joined
curl -s http://localhost:4000/trainers/1/appointments | jq

# Available 30-min slots for trainer 1, next Monday in PT
curl -s "http://localhost:4000/trainers/1/slots?starts_at=2026-05-04T00:00:00-07:00&ends_at=2026-05-04T23:59:59-07:00" | jq
```

### 4. Useful URLs

| URL | What |
|---|---|
| <http://localhost:4000> | API base |
| <http://localhost:9400> | Encore dev dashboard — traces, request runner, DB browser, schema |

### 5. Tests

```bash
encore test ./...
```

Encore spins up an isolated test database (auto-migrated, separate from
your dev DB), then runs the full Go test suite. ~17 tests / ~28 subtests
covering:

- **Pure-function unit tests** ([helpers_test.go](appointments/helpers_test.go))
  for `generateSlots`, `withinAvailability`, `blocksFromRows`,
  `isUniqueViolation` — table-driven, no DB.
- **Handler tests** ([book_test.go](appointments/book_test.go),
  [slots_test.go](appointments/slots_test.go),
  [list_test.go](appointments/list_test.go)) calling `Book`, `GetSlots`,
  and `ListAppointments` against the real handlers + test DB. Covers the
  full validation matrix (weekend, before/after hours, wrong duration,
  off-boundary, past, missing fields, unknown trainer, unknown timezone),
  the booked-slot exclusion path, JOIN ordering, and timezone-override
  formatting.

Tests truncate the `appointments` table between cases (shared DB +
truncation pattern) and use deterministic `futureWeekday` /
`futureWeekend` helpers so they don't depend on the wall clock.

### 6. Useful commands

```bash
encore run                        # start (auto-reloads on save)
encore test ./...                 # run the test suite
encore db shell appointments      # psql into the local DB
encore db reset appointments      # drop + recreate; restart `encore run` to reseed
encore check                      # static check (compile + lint)
cd appointments && sqlc generate  # regenerate db/*.go after editing query.sql or migrations
```

> **Reseeding after a DB reset:** `encore db reset` clears the database
> but the service `init()` only runs at process startup, so stop and
> restart `encore run` to repopulate seed data.

## Why Encore

- Auto-provisioned Postgres in dev — no `docker compose` to babysit.
- Built-in tracing + dev dashboard make endpoint debugging trivial.
- Declarative infra (`sqldb.NewDatabase`) keeps the service definition
  in one place; the same code runs against managed Postgres in any cloud.
- Pub/Sub and Temporal integrations are first-class — easy upgrade path
  if this service grows notification or multi-step workflow needs.

## Libraries

| Library                | Purpose                                                         |
| ---------------------- | --------------------------------------------------------------- |
| `encore.dev`           | Service framework, sqldb, errs, structured logging              |
| `pgx/v5` + `pgtype`    | Postgres driver; `Timestamptz` round-trips with TZ preserved    |
| `sqlc`                 | Code-gen typed query layer from `db/query.sql`                  |
| `time/tzdata`          | Embed IANA tz database in the binary (Alpine has no tz data)    |

## Time handling

The whole service revolves around one rule: **wall-clock checks happen in
the trainer's local timezone, storage happens in UTC.**

1. Storage: every time column is `TIMESTAMPTZ`. Postgres stores UTC, pgx
   round-trips with the original instant intact.
2. Parsing: Encore decodes JSON `time.Time` with whatever offset the
   client sent. We never assume UTC — we immediately do
   `t.In(loc)` against the trainer's `time.LoadLocation(...)` before any
   `Hour`/`Minute`/`Weekday` check.
3. Slot construction: built directly in local time via
   `time.Date(y, m, d, h, m, 0, 0, loc)`. Building in UTC and converting
   produces wrong times on DST transition days.
4. `time.LoadLocation` (IANA), never `time.FixedZone`. A fixed offset
   doesn't know about DST and silently drifts twice a year.
5. `import _ "time/tzdata"` so the IANA database is embedded — Alpine
   containers ship without it and `LoadLocation` would fail at runtime.
6. **Display timezone is a client concern.** The slots endpoint accepts
   an optional `timezone` query param so callers can render in the
   user's zone, but the trainer's zone is the source of truth for
   business-hour validation. The API never tries to guess.

## Seed strategy

Two mechanisms, each suited to its data:

- **`appointments/db/fixtures.sql`** — static reference data
  (trainers, users, weekday availability). Pure SQL, idempotent via
  `ON CONFLICT DO NOTHING`. Loaded with one `sqldb.Exec`.
- **`appointments/appointments.json`** — the dataset Future provided.
  Embedded via `go:embed`, parsed in Go because the records arrive as
  RFC3339 strings that need `time.Parse` → `pgtype.Timestamptz`
  conversion before insert. Inserts use `ON CONFLICT (trainer_id,
  started_at) DO NOTHING` to stay idempotent. Bypasses business-hour
  validation — some seed records fall on weekends (Jan 26 2019 is a
  Saturday), which is intentional historical data.

Both are gated behind `encore.Meta().Environment.Cloud == encore.CloudLocal`
so seed code is a no-op in any deployed environment.

## Availability model

Per-trainer, per-weekday rows in the `availability` table:

```
trainer_id | weekday (0=Sun..6=Sat) | start_time | end_time
```

No unique constraint on `(trainer_id, weekday)` — multiple rows per day
are supported, so future use cases like a midday break ("9–12, 1–5")
work without a schema change. NULL `start_time`/`end_time` means the
trainer is unavailable that weekday. Default seed is M–F 08:00–17:00 to
match the assignment.

## Algorithm & concurrency

The whole service leans on one product invariant from the spec:

> *All appointments are 30 minutes long, and should be scheduled at :00, :30
> minutes after the hour during business hours.*

That single sentence collapses the geometry of the problem. **Two
appointments for the same trainer can only conflict in one way: identical
`starts_at`.** There is no off-grid case, no partial overlap, no
half-hour-shifted collision. Once you see that, two pieces of "obvious"
defensive code disappear:

### Slot generation — hash-set lookup, single pass

`generateSlots` ([appointments/helpers.go](appointments/helpers.go)) builds
30-minute slots day-by-day in the trainer's local timezone, checking three
conditions inline:

1. The slot fits inside the requested `[starts_at, ends_at]` window.
2. The slot starts in the future (`!slot.Before(time.Now())`).
3. The slot's start instant isn't in the `booked` hash set.

The booked set is built once from the `GetAppointmentsByTrainerBetween`
result — `map[int64]struct{}` keyed on `b.StartsAt.Time.Unix()`. Membership
is O(1). Total: **O(D·B·S + K)**, where D = days, B = blocks/day, S =
slots/block, K = booked appointments — versus the naive O(D·B·S·K) that
overlap-scans every slot against every booking. There's no separate
`filterBookedSlots` or `filterPast` pass; both checks happen in the same
loop that emits slots.

### Booking — the unique index *is* the safety net

`book.go` does no application-level overlap query and runs no transaction.
A successful booking is a single statement:

```go
created, err := query.InsertAppointment(ctx, db.InsertAppointmentParams{...})
if isUniqueViolation(err) {
    return errs.AlreadyExists
}
```

The migration declares
`CREATE UNIQUE INDEX appointments_trainer_time_idx ON appointments
(trainer_id, starts_at)` — Postgres rejects any conflict atomically with a
`23505` unique violation. There is no SELECT-then-INSERT TOCTOU window
because there is no SELECT. Concurrent identical bookings race against the
index, exactly one wins, the rest map to `errs.AlreadyExists` with the same
message a slow application-level check would have produced.

### Upgrade path: variable-duration appointments

The day product asks for, say, 60-minute sessions for senior trainers, the
unique index is no longer sufficient — a 60-min booking at 9:00 conflicts
with a 30-min booking at 9:30, but their `starts_at` differ. The fix is
mechanical, not architectural:

1. **Migration** — swap the unique index for a Postgres **EXCLUSION
   constraint** that rejects any two range-overlapping appointments for the
   same trainer:

   ```sql
   ALTER TABLE appointments
     DROP CONSTRAINT appointments_trainer_time_idx,
     ADD CONSTRAINT appointments_no_overlap
       EXCLUDE USING gist (
         trainer_id WITH =,
         tstzrange(starts_at, ends_at, '[)') WITH &&
       );
   ```

2. **`isUniqueViolation`** — add the `23P01` exclusion-violation code.
3. **Slot generation** — the booked set becomes a *covered cells* set: for
   each booking, mark every 30-min cell from `starts_at` to `ends_at`. A
   60-min booking at 9:00 marks {9:00, 9:30}. Lookup stays O(1).
4. **Validation** — replace `if d != SlotDuration` with an allowed-set
   check.

Total: ~10 LOC plus one migration. The application code stays thin because
the safety net was always the database constraint, not application logic;
only the constraint primitive changes (point uniqueness → range exclusion).

## What I'd add with more time

- Pagination on `/trainers/:id/appointments`.
- Cancellation: soft-delete column on `appointments` plus a DELETE
  endpoint, so the unique `(trainer_id, started_at)` index becomes a
  partial index over live rows.
- Date-specific availability overrides table (vacations, one-off blocks).
- Idempotency keys on `POST /appointments` so retries from flaky clients
  don't produce ambiguity even before the unique constraint trips.
- Pub/Sub publish on successful booking → email/SMS handler with an
  idempotent consumer (Encore makes this a few lines).
- Temporal-driven workflow for multi-step booking flows (hold slot →
  collect payment → confirm), with the booking endpoint enqueueing
  rather than writing directly.
- Concurrency stress tests on `Book` (fire N parallel identical POSTs,
  assert exactly one wins) — the unique index already provides this
  guarantee, but a load test would prove it under real contention.
- Property-based / fuzz tests on `generateSlots` (random availability
  shapes + random booked sets, invariant: no returned slot is past, in
  the booked set, or outside the window).
