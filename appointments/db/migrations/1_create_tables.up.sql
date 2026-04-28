CREATE TABLE trainers (
    id       SERIAL PRIMARY KEY,
    name     TEXT NOT NULL,
    email    TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'America/Los_Angeles'
);

CREATE TABLE users (
    id    SERIAL PRIMARY KEY,
    name  TEXT NOT NULL,
    email TEXT NOT NULL
);

CREATE TABLE availability (
    id         SERIAL PRIMARY KEY,
    trainer_id INTEGER NOT NULL REFERENCES trainers(id),
    weekday    SMALLINT NOT NULL,
    start_time TIME,
    end_time   TIME
);

CREATE INDEX availability_trainer_weekday_idx
    ON availability (trainer_id, weekday);

CREATE TABLE appointments (
    id         BIGSERIAL PRIMARY KEY,
    trainer_id INTEGER NOT NULL REFERENCES trainers(id),
    user_id    INTEGER NOT NULL REFERENCES users(id),
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX appointments_trainer_time_idx
    ON appointments (trainer_id, starts_at);

CREATE INDEX appointments_trainer_range_idx
    ON appointments (trainer_id, starts_at, ends_at);
