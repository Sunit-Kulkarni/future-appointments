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
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type Appointment struct {
	ID        int64  `json:"id"`
	TrainerID int32  `json:"trainer_id"`
	UserID    int32  `json:"user_id"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
	Timezone  string `json:"timezone"`
}

//encore:api public method=POST path=/appointments
func Book(ctx context.Context, p *BookParams) (*Appointment, error) {
	eb := errs.B()

	if p.TrainerID == 0 || p.UserID == 0 {
		return nil, eb.Code(errs.InvalidArgument).Msg("trainer_id and user_id are required").Err()
	}
	if p.StartsAt.IsZero() || p.EndsAt.IsZero() {
		return nil, eb.Code(errs.InvalidArgument).Msg("starts_at and ends_at are required").Err()
	}

	trainer, err := query.GetTrainerByID(ctx, p.TrainerID)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.NotFound).Msg("trainer not found").Err()
	}

	loc, err := time.LoadLocation(trainer.Timezone)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Internal).Msg("invalid trainer timezone").Err()
	}

	localStart := p.StartsAt.In(loc)
	localEnd := p.EndsAt.In(loc)

	if !p.StartsAt.After(time.Now()) {
		return nil, eb.Code(errs.InvalidArgument).Msg("starts_at must be in the future").Err()
	}
	if d := localEnd.Sub(localStart); d != SlotDuration {
		return nil, eb.Code(errs.InvalidArgument).Msgf("appointment duration must be %s, got %s", SlotDuration, d).Err()
	}
	if localStart.Second() != 0 || localStart.Nanosecond() != 0 || (localStart.Minute() != 0 && localStart.Minute() != 30) {
		return nil, eb.Code(errs.InvalidArgument).Msg("starts_at must align to a :00 or :30 boundary").Err()
	}

	availRows, err := query.GetAvailabilityByTrainerAndWeekday(ctx, db.GetAvailabilityByTrainerAndWeekdayParams{
		TrainerID: p.TrainerID,
		Weekday:   localStart.Weekday(),
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

	// No application-level overlap check, no transaction. The unique index
	// `(trainer_id, starts_at)` is the single source of truth: with the spec's
	// alignment invariant (30-min appointments on :00/:30) the only conflict
	// mode is identical starts_at, which the index rejects atomically. A
	// SELECT-then-INSERT pair would only add a TOCTOU window. See README
	// "Algorithm & concurrency".
	created, err := query.InsertAppointment(ctx, db.InsertAppointmentParams{
		TrainerID: p.TrainerID,
		UserID:    p.UserID,
		StartsAt:  pgtype.Timestamptz{Time: p.StartsAt, Valid: true},
		EndsAt:    pgtype.Timestamptz{Time: p.EndsAt, Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, eb.Code(errs.AlreadyExists).Msg("slot is already booked").Err()
		}
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to insert appointment").Err()
	}

	return &Appointment{
		ID:        created.ID,
		TrainerID: created.TrainerID,
		UserID:    created.UserID,
		StartsAt: created.StartsAt.Time.In(loc).Format(time.RFC3339),
		EndsAt:   created.EndsAt.Time.In(loc).Format(time.RFC3339),
		Timezone:  trainer.Timezone,
	}, nil
}
