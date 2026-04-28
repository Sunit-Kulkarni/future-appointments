package appointments

import (
	"context"
	"time"

	"encore.dev/beta/errs"
)

type AppointmentDetail struct {
	ID          int64  `json:"id"`
	TrainerID   int32  `json:"trainer_id"`
	TrainerName string `json:"trainer_name"`
	UserID      int32  `json:"user_id"`
	UserName    string `json:"user_name"`
	StartedAt   string `json:"started_at"`
	EndedAt     string `json:"ended_at"`
	Timezone    string `json:"timezone"`
}

type ListAppointmentsResponse struct {
	Appointments []AppointmentDetail `json:"appointments"`
}

//encore:api public method=GET path=/trainers/:trainerID/appointments
func ListAppointments(ctx context.Context, trainerID int32) (*ListAppointmentsResponse, error) {
	eb := errs.B()

	rows, err := query.GetAppointmentsByTrainer(ctx, trainerID)
	if err != nil {
		return nil, eb.Cause(err).Code(errs.Unavailable).Msg("failed to load appointments").Err()
	}

	out := make([]AppointmentDetail, 0, len(rows))
	locCache := make(map[string]*time.Location)
	for _, r := range rows {
		loc, ok := locCache[r.TrainerTimezone]
		if !ok {
			loc, err = time.LoadLocation(r.TrainerTimezone)
			if err != nil {
				return nil, eb.Cause(err).Code(errs.Internal).Msgf("invalid trainer timezone %q", r.TrainerTimezone).Err()
			}
			locCache[r.TrainerTimezone] = loc
		}
		out = append(out, AppointmentDetail{
			ID:          r.ID,
			TrainerID:   r.TrainerID,
			TrainerName: r.TrainerName,
			UserID:      r.UserID,
			UserName:    r.UserName,
			StartedAt:   r.StartedAt.Time.In(loc).Format(time.RFC3339),
			EndedAt:     r.EndedAt.Time.In(loc).Format(time.RFC3339),
			Timezone:    r.TrainerTimezone,
		})
	}

	return &ListAppointmentsResponse{Appointments: out}, nil
}
