package appointments

import (
	"context"
	"errors"
	"testing"
	"time"

	"encore.dev/beta/errs"
)

func TestBook_HappyPath(t *testing.T) {
	truncateAppointments(t)
	mon := futureWeekday(t)
	loc := mon.Location()
	start := time.Date(mon.Year(), mon.Month(), mon.Day(), 9, 0, 0, 0, loc)
	end := start.Add(30 * time.Minute)

	got, err := Book(context.Background(), &BookParams{
		TrainerID: 1,
		UserID:    1,
		StartsAt:  start,
		EndsAt:    end,
	})
	if err != nil {
		t.Fatalf("Book returned error: %v", err)
	}
	if got.ID == 0 || got.TrainerID != 1 || got.UserID != 1 {
		t.Errorf("unexpected response: %+v", got)
	}
	if got.Timezone != "America/Los_Angeles" {
		t.Errorf("timezone = %q, want America/Los_Angeles", got.Timezone)
	}
	// Times should round-trip through the trainer's local zone (-07:00 PDT
	// or -08:00 PST depending on date).
	if _, parseErr := time.Parse(time.RFC3339, got.StartsAt); parseErr != nil {
		t.Errorf("starts_at not RFC3339: %q (%v)", got.StartsAt, parseErr)
	}
}

func TestBook_DuplicateReturnsAlreadyExists(t *testing.T) {
	truncateAppointments(t)
	mon := futureWeekday(t)
	loc := mon.Location()
	start := time.Date(mon.Year(), mon.Month(), mon.Day(), 10, 0, 0, 0, loc)

	params := &BookParams{
		TrainerID: 1, UserID: 1,
		StartsAt: start,
		EndsAt:   start.Add(30 * time.Minute),
	}
	if _, err := Book(context.Background(), params); err != nil {
		t.Fatalf("first Book failed: %v", err)
	}
	_, err := Book(context.Background(), params)
	if err == nil {
		t.Fatalf("second Book succeeded; want AlreadyExists")
	}
	if errs.Code(err) != errs.AlreadyExists {
		t.Errorf("err code = %v, want AlreadyExists; err: %v", errs.Code(err), err)
	}
}

func TestBook_UnknownTrainerReturnsNotFound(t *testing.T) {
	mon := futureWeekday(t)
	start := time.Date(mon.Year(), mon.Month(), mon.Day(), 9, 0, 0, 0, mon.Location())
	_, err := Book(context.Background(), &BookParams{
		TrainerID: 9999, UserID: 1,
		StartsAt: start, EndsAt: start.Add(30 * time.Minute),
	})
	if err == nil || errs.Code(err) != errs.NotFound {
		t.Errorf("err = %v (code %v), want NotFound", err, errs.Code(err))
	}
}

func TestBook_Validation(t *testing.T) {
	mon := futureWeekday(t)
	sat := futureWeekend(t)
	loc := mon.Location()
	at := func(d time.Time, h, m int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
	}

	cases := []struct {
		name             string
		starts, ends     time.Time
		trainerID, userID int32
		wantCode         errs.ErrCode
	}{
		{
			name: "weekend Saturday",
			starts: at(sat, 9, 0), ends: at(sat, 9, 30),
			trainerID: 1, userID: 1,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "before business hours",
			starts: at(mon, 7, 30), ends: at(mon, 8, 0),
			trainerID: 1, userID: 1,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "after business hours",
			starts: at(mon, 17, 0), ends: at(mon, 17, 30),
			trainerID: 1, userID: 1,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "duration 45 minutes",
			starts: at(mon, 11, 0), ends: at(mon, 11, 45),
			trainerID: 1, userID: 1,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "off-boundary :15",
			starts: at(mon, 9, 15), ends: at(mon, 9, 45),
			trainerID: 1, userID: 1,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "in the past",
			starts: time.Date(2019, 2, 4, 9, 0, 0, 0, loc),
			ends:   time.Date(2019, 2, 4, 9, 30, 0, 0, loc),
			trainerID: 1, userID: 1,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "missing trainer_id",
			starts: at(mon, 9, 0), ends: at(mon, 9, 30),
			trainerID: 0, userID: 1,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "missing user_id",
			starts: at(mon, 9, 0), ends: at(mon, 9, 30),
			trainerID: 1, userID: 0,
			wantCode: errs.InvalidArgument,
		},
		{
			name: "zero starts_at",
			starts: time.Time{}, ends: at(mon, 9, 30),
			trainerID: 1, userID: 1,
			wantCode: errs.InvalidArgument,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Book(context.Background(), &BookParams{
				TrainerID: c.trainerID, UserID: c.userID,
				StartsAt: c.starts, EndsAt: c.ends,
			})
			if err == nil {
				t.Fatalf("want error code %v, got nil", c.wantCode)
			}
			if got := errs.Code(err); got != c.wantCode {
				t.Errorf("err code = %v, want %v; err: %v", got, c.wantCode, err)
			}
		})
	}

	// silence unused if cases ever shrink to zero
	_ = errors.New
}
