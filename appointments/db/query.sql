-- name: GetTrainerByID :one
SELECT * FROM trainers WHERE id = $1;

-- name: GetAvailabilityByTrainerAndWeekday :many
SELECT * FROM availability
WHERE trainer_id = $1 AND weekday = $2
ORDER BY start_time;

-- name: GetAvailabilityByTrainer :many
SELECT * FROM availability
WHERE trainer_id = $1
ORDER BY weekday, start_time;

-- name: InsertAppointment :one
INSERT INTO appointments (trainer_id, user_id, starts_at, ends_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAppointmentsByTrainer :many
SELECT
    a.id,
    a.starts_at,
    a.ends_at,
    a.created_at,
    t.id       AS trainer_id,
    t.name     AS trainer_name,
    t.timezone AS trainer_timezone,
    u.id       AS user_id,
    u.name     AS user_name
FROM appointments a
JOIN trainers t ON t.id = a.trainer_id
JOIN users    u ON u.id = a.user_id
WHERE a.trainer_id = $1
ORDER BY a.starts_at;

-- name: GetAppointmentsByTrainerBetween :many
SELECT * FROM appointments
WHERE trainer_id = $1
  AND starts_at >= $2
  AND ends_at   <= $3
ORDER BY starts_at;

-- name: DeleteAppointment :exec
DELETE FROM appointments WHERE id = $1;
