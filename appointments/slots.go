package appointments

import (
	"context"
	"time"

	"encore.app/appointments/db"

	"encore.dev/beta/errs"

	"github.com/jackc/pgx/v5/pgtype"
)

type bookableSlot struct {
	Start time.Time
	End   time.Time
}

type Slot struct {
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}

type GetSlotsParams struct {
	StartsAt time.Time `query:"starts_at"`
	EndsAt   time.Time `query:"ends_at"`
	Timezone string    `query:"timezone"`
}

type GetSlotsResponse struct {
	TrainerID int32  `json:"trainer_id"`
	Timezone  string `json:"timezone"`
	Slots     []Slot `json:"slots"`
}

//encore:api public method=GET path=/trainers/:trainerID/slots
func GetSlots(ctx context.Context, trainerID int32, p *GetSlotsParams) (*GetSlotsResponse, error) {
	eb := errs.B()

	if p.StartsAt.IsZero() || p.EndsAt.IsZero() {
		return nil, eb.Code(errs.InvalidArgument).Msg("starts_at and ends_at are required").Err()
	}
	if !p.StartsAt.Before(p.EndsAt) {
		return nil, eb.Code(errs.InvalidArgument).Msg("starts_at must be before ends_at").Err()
	}

	trainer, err := query.GetTrainerByID(ctx, trainerID)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.NotFound).Msg("trainer not found").Err()
	}

	trainerLoc, err := time.LoadLocation(trainer.Timezone)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Internal).Msg("invalid trainer timezone").Err()
	}

	respLoc := trainerLoc
	if p.Timezone != "" {
		respLoc, err = time.LoadLocation(p.Timezone)
		if err != nil {
			return nil, eb.Cause(err).Code(errs.InvalidArgument).Msg("invalid timezone").Err()
		}
	}

	availRows, err := query.GetAvailabilityByTrainer(ctx, trainerID)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to load availability").Err()
	}

	byWeekday := make(map[time.Weekday][]availabilityBlock)
	for _, row := range availRows {
		block, ok := blockFromRow(row)
		if !ok {
			continue
		}
		byWeekday[row.Weekday] = append(byWeekday[row.Weekday], block)
	}

	bookedRows, err := query.GetAppointmentsByTrainerBetween(ctx, db.GetAppointmentsByTrainerBetweenParams{
		TrainerID: trainerID,
		StartsAt:  pgtype.Timestamptz{Time: p.StartsAt, Valid: true},
		EndsAt:    pgtype.Timestamptz{Time: p.EndsAt, Valid: true},
	})
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to load appointments").Err()
	}

	booked := make(map[int64]struct{}, len(bookedRows))
	for _, b := range bookedRows {
		booked[b.StartsAt.Time.Unix()] = struct{}{}
	}

	slots := generateSlots(p.StartsAt, p.EndsAt, byWeekday, trainerLoc, booked, time.Now())

	out := make([]Slot, 0, len(slots))
	for _, s := range slots {
		out = append(out, Slot{
			StartsAt: s.Start.In(respLoc).Format(time.RFC3339),
			EndsAt:   s.End.In(respLoc).Format(time.RFC3339),
		})
	}

	return &GetSlotsResponse{
		TrainerID: trainerID,
		Timezone:  respLoc.String(),
		Slots:     out,
	}, nil
}
