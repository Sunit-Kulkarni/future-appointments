package appointments

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestListAppointments_EmptyTrainerReturnsEmpty(t *testing.T) {
	truncateAppointments(t)
	resp, err := ListAppointments(context.Background(), 2)
	if err != nil {
		t.Fatalf("ListAppointments: %v", err)
	}
	if len(resp.Appointments) != 0 {
		t.Errorf("want 0 appointments for unbooked trainer, got %d", len(resp.Appointments))
	}
}

func TestListAppointments_JoinAndOrder(t *testing.T) {
	truncateAppointments(t)
	mon := futureWeekday(t)
	loc := mon.Location()

	// Book three slots out of order: 11:00, 09:00, 10:00. The handler must
	// return them sorted ascending by starts_at.
	times := []int{11, 9, 10}
	for _, h := range times {
		start := time.Date(mon.Year(), mon.Month(), mon.Day(), h, 0, 0, 0, loc)
		if _, err := Book(context.Background(), &BookParams{
			TrainerID: 2, UserID: 3,
			StartsAt: start, EndsAt: start.Add(30 * time.Minute),
		}); err != nil {
			t.Fatalf("Book at %d:00: %v", h, err)
		}
	}

	resp, err := ListAppointments(context.Background(), 2)
	if err != nil {
		t.Fatalf("ListAppointments: %v", err)
	}
	if len(resp.Appointments) != 3 {
		t.Fatalf("want 3 appointments, got %d", len(resp.Appointments))
	}

	// Verify ordering by parsing the formatted timestamps.
	parsed := make([]time.Time, len(resp.Appointments))
	for i, a := range resp.Appointments {
		ts, perr := time.Parse(time.RFC3339, a.StartsAt)
		if perr != nil {
			t.Fatalf("appointment %d starts_at not RFC3339: %q", i, a.StartsAt)
		}
		parsed[i] = ts
	}
	if !sort.SliceIsSorted(parsed, func(i, j int) bool { return parsed[i].Before(parsed[j]) }) {
		t.Errorf("appointments not sorted ascending by starts_at: %v", parsed)
	}

	// Verify JOIN populated trainer + user names.
	a := resp.Appointments[0]
	if a.TrainerName != "Bob Smith" {
		t.Errorf("trainer_name = %q, want Bob Smith", a.TrainerName)
	}
	if a.UserName == "" {
		t.Errorf("user_name empty (JOIN failed)")
	}
	if a.Timezone != "America/Los_Angeles" {
		t.Errorf("timezone = %q, want America/Los_Angeles", a.Timezone)
	}
}

func TestListAppointments_TimesInTrainerLocalZone(t *testing.T) {
	truncateAppointments(t)
	mon := futureWeekday(t)
	loc := mon.Location()
	start := time.Date(mon.Year(), mon.Month(), mon.Day(), 14, 0, 0, 0, loc)

	if _, err := Book(context.Background(), &BookParams{
		TrainerID: 1, UserID: 1,
		StartsAt: start, EndsAt: start.Add(30 * time.Minute),
	}); err != nil {
		t.Fatalf("Book: %v", err)
	}

	resp, err := ListAppointments(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListAppointments: %v", err)
	}
	if len(resp.Appointments) != 1 {
		t.Fatalf("want 1 appointment, got %d", len(resp.Appointments))
	}
	got, perr := time.Parse(time.RFC3339, resp.Appointments[0].StartsAt)
	if perr != nil {
		t.Fatalf("parse starts_at: %v", perr)
	}
	// Verify the wire format carries the trainer's local offset (PT — either
	// -07:00 PDT or -08:00 PST depending on the calendar date).
	_, offsetSec := got.Zone()
	if offsetSec != -7*3600 && offsetSec != -8*3600 {
		t.Errorf("starts_at offset %ds, want -7h or -8h (Pacific)", offsetSec)
	}
	if !got.Equal(start) {
		t.Errorf("starts_at instant differs: got %v, want %v", got, start)
	}
}
