package appointments

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestMain loads the static fixtures (trainers/users/availability) once
// before any test runs. Each test that touches the appointments table is
// expected to call truncateAppointments at the start to start clean.
//
// We deliberately skip the package init() seed in test mode (see
// appointments.go) so tests have full control over DB state and don't
// race against a background goroutine.
func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := appointmentsDB.Exec(ctx, fixturesSQL); err != nil {
		panic("test setup: load fixtures: " + err.Error())
	}
	os.Exit(m.Run())
}

// truncateAppointments removes every row from the appointments table and
// resets the BIGSERIAL counter, leaving trainers/users/availability intact.
// Use at the start of any test that exercises Book/ListAppointments/GetSlots
// where booked-appointment state matters.
func truncateAppointments(t *testing.T) {
	t.Helper()
	if _, err := appointmentsDB.Exec(context.Background(),
		`TRUNCATE appointments RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate appointments: %v", err)
	}
}

// futureWeekday returns the next future Monday-Friday calendar date in PT
// at midnight. Tests use this to build deterministic, future-dated inputs
// without depending on the wall clock.
func futureWeekday(t *testing.T) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	// Anchor 60 days out so even tests that book multiple slots stay future.
	d := time.Now().In(loc).AddDate(0, 0, 60)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// futureWeekend returns the next future Saturday in PT at midnight.
func futureWeekend(t *testing.T) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	d := time.Now().In(loc).AddDate(0, 0, 60)
	for d.Weekday() != time.Saturday {
		d = d.AddDate(0, 0, 1)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}
