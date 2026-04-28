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

## Running locally

Prerequisites: [Encore CLI](https://encore.dev/docs/install) (`brew install
encoredev/tap/encore`) and Docker Desktop.

```bash
encore run
```

- API: <http://localhost:4000>
- Dev dashboard (traces, schemas, request runner): <http://localhost:9400>

> **No Dockerfile needed.** The take-home doc mentions a Dockerfile would
> be appreciated; Encore is the "or equivalent" — `encore run` provisions
> Postgres in Docker for you, applies migrations, runs the seed init,
> and exposes the dev dashboard. No `docker compose` or hand-written
> Dockerfile to maintain.

On first boot Encore provisions Postgres in Docker, applies migrations,
and the service's `init()` seeds `fixtures.sql` (3 trainers, 10 users,
M–F 08:00–17:00 availability) plus the appointments from
`appointments/appointments.json`. Seeding is gated to
`encore.CloudLocal`, so it never runs in staging or production.

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
- A seed-data-aware test harness running against an ephemeral Encore
  test DB; right now correctness is verified manually via the dev
  dashboard.
