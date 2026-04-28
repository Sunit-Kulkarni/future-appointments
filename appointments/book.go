package appointments

import (
	"context"
	"time"

	"encore.app/appointments/db"

	"encore.dev/beta/errs"

	"github.com/jackc/pgx/v5/pgtype"
)

type BookParams struct {
	TrainerID int32     `json:"trainer_id"`
	UserID    int32     `json:"user_id"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

type Appointment struct {
	ID        int64  `json:"id"`
	TrainerID int32  `json:"trainer_id"`
	UserID    int32  `json:"user_id"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	Timezone  string `json:"timezone"`
}

//encore:api public method=POST path=/appointments
func Book(ctx context.Context, p *BookParams) (*Appointment, error) {
	eb := errs.B()

	if p.TrainerID == 0 || p.UserID == 0 {
		return nil, eb.Code(errs.InvalidArgument).Msg("trainer_id and user_id are required").Err()
	}
	if p.StartedAt.IsZero() || p.EndedAt.IsZero() {
		return nil, eb.Code(errs.InvalidArgument).Msg("started_at and ended_at are required").Err()
	}

	trainer, err := query.GetTrainerByID(ctx, p.TrainerID)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.NotFound).Msg("trainer not found").Err()
	}

	loc, err := time.LoadLocation(trainer.Timezone)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Internal).Msg("invalid trainer timezone").Err()
	}

	localStart := p.StartedAt.In(loc)
	localEnd := p.EndedAt.In(loc)

	if !p.StartedAt.After(time.Now()) {
		return nil, eb.Code(errs.InvalidArgument).Msg("started_at must be in the future").Err()
	}
	if d := localEnd.Sub(localStart); d != SlotDuration {
		return nil, eb.Code(errs.InvalidArgument).Msgf("appointment duration must be %s, got %s", SlotDuration, d).Err()
	}
	if localStart.Second() != 0 || localStart.Nanosecond() != 0 || (localStart.Minute() != 0 && localStart.Minute() != 30) {
		return nil, eb.Code(errs.InvalidArgument).Msg("started_at must align to a :00 or :30 boundary").Err()
	}

	availRows, err := query.GetAvailabilityByTrainerAndWeekday(ctx, db.GetAvailabilityByTrainerAndWeekdayParams{
		TrainerID: p.TrainerID,
		Weekday:   int16(localStart.Weekday()),
	})
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to load availability").Err()
	}
	blocks := blocksFromRows(availRows)
	if len(blocks) == 0 {
		return nil, eb.Code(errs.InvalidArgument).Msgf("trainer is not available on %s", localStart.Weekday()).Err()
	}
	if !withinAvailability(localStart, localEnd, blocks) {
		return nil, eb.Code(errs.InvalidArgument).Msg("requested time is outside trainer availability").Err()
	}

	tx, err := pgxdb.Begin(ctx)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to start transaction").Err()
	}
	defer tx.Rollback(context.Background())

	q := query.WithTx(tx)

	overlap, err := q.GetOverlappingAppointments(ctx, db.GetOverlappingAppointmentsParams{
		TrainerID: p.TrainerID,
		StartedAt: pgtype.Timestamptz{Time: p.EndedAt, Valid: true},
		EndedAt:   pgtype.Timestamptz{Time: p.StartedAt, Valid: true},
	})
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("overlap check failed").Err()
	}
	if len(overlap) > 0 {
		return nil, eb.Code(errs.AlreadyExists).Msg("slot is already booked").Err()
	}

	created, err := q.InsertAppointment(ctx, db.InsertAppointmentParams{
		TrainerID: p.TrainerID,
		UserID:    p.UserID,
		StartedAt: pgtype.Timestamptz{Time: p.StartedAt, Valid: true},
		EndedAt:   pgtype.Timestamptz{Time: p.EndedAt, Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, eb.Code(errs.AlreadyExists).Msg("slot is already booked").Err()
		}
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to insert appointment").Err()
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to commit transaction").Err()
	}

	return &Appointment{
		ID:        created.ID,
		TrainerID: created.TrainerID,
		UserID:    created.UserID,
		StartedAt: created.StartedAt.Time.In(loc).Format(time.RFC3339),
		EndedAt:   created.EndedAt.Time.In(loc).Format(time.RFC3339),
		Timezone:  trainer.Timezone,
	}, nil
}
