package appointments

import (
	"context"
	"testing"
	"time"

	"encore.dev/beta/errs"
)

func TestGetSlots_BusinessHoursMonday(t *testing.T) {
	truncateAppointments(t)
	mon := futureWeekday(t)
	end := mon.Add(24 * time.Hour)

	resp, err := GetSlots(context.Background(), 1, &GetSlotsParams{
		StartsAt: mon,
		EndsAt:   end,
	})
	if err != nil {
		t.Fatalf("GetSlots: %v", err)
	}
	if len(resp.Slots) != 18 {
		t.Errorf("want 18 slots on a weekday, got %d", len(resp.Slots))
	}
	if resp.Timezone != "America/Los_Angeles" {
		t.Errorf("default response timezone = %q, want trainer's", resp.Timezone)
	}
}

func TestGetSlots_WeekendIsEmpty(t *testing.T) {
	truncateAppointments(t)
	sat := futureWeekend(t)
	resp, err := GetSlots(context.Background(), 1, &GetSlotsParams{
		StartsAt: sat,
		EndsAt:   sat.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("GetSlots: %v", err)
	}
	if len(resp.Slots) != 0 {
		t.Errorf("want 0 slots on Saturday, got %d", len(resp.Slots))
	}
}

func TestGetSlots_ExcludesBookedSlot(t *testing.T) {
	truncateAppointments(t)
	mon := futureWeekday(t)
	end := mon.Add(24 * time.Hour)
	loc := mon.Location()

	before, err := GetSlots(context.Background(), 1, &GetSlotsParams{StartsAt: mon, EndsAt: end})
	if err != nil {
		t.Fatalf("GetSlots before: %v", err)
	}

	// Book exactly one slot at 09:00 PT.
	booked := time.Date(mon.Year(), mon.Month(), mon.Day(), 9, 0, 0, 0, loc)
	if _, err := Book(context.Background(), &BookParams{
		TrainerID: 1, UserID: 1,
		StartsAt: booked, EndsAt: booked.Add(30 * time.Minute),
	}); err != nil {
		t.Fatalf("Book: %v", err)
	}

	after, err := GetSlots(context.Background(), 1, &GetSlotsParams{StartsAt: mon, EndsAt: end})
	if err != nil {
		t.Fatalf("GetSlots after: %v", err)
	}
	if len(after.Slots) != len(before.Slots)-1 {
		t.Errorf("slot count: before=%d after=%d (want diff -1)", len(before.Slots), len(after.Slots))
	}
	for _, s := range after.Slots {
		if s.StartsAt == booked.Format(time.RFC3339) {
			t.Errorf("booked slot %q leaked through", s.StartsAt)
		}
	}
}

func TestGetSlots_TimezoneOverride(t *testing.T) {
	truncateAppointments(t)
	mon := futureWeekday(t)
	end := mon.Add(24 * time.Hour)

	pt, err := GetSlots(context.Background(), 1, &GetSlotsParams{StartsAt: mon, EndsAt: end})
	if err != nil {
		t.Fatalf("GetSlots PT: %v", err)
	}
	et, err := GetSlots(context.Background(), 1, &GetSlotsParams{
		StartsAt: mon, EndsAt: end, Timezone: "America/New_York",
	})
	if err != nil {
		t.Fatalf("GetSlots ET: %v", err)
	}

	if len(pt.Slots) != len(et.Slots) {
		t.Errorf("slot count differs: PT=%d ET=%d (count must be tz-independent)",
			len(pt.Slots), len(et.Slots))
	}
	if pt.Timezone == et.Timezone {
		t.Errorf("response timezone strings unchanged: %q vs %q", pt.Timezone, et.Timezone)
	}
	if et.Timezone != "America/New_York" {
		t.Errorf("override timezone = %q, want America/New_York", et.Timezone)
	}
}

func TestGetSlots_RequiredParams(t *testing.T) {
	cases := []struct {
		name             string
		starts, ends     time.Time
	}{
		{"missing starts_at", time.Time{}, futureWeekday(t).Add(24 * time.Hour)},
		{"missing ends_at", futureWeekday(t), time.Time{}},
		{"starts after ends", futureWeekday(t).Add(24 * time.Hour), futureWeekday(t)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := GetSlots(context.Background(), 1, &GetSlotsParams{
				StartsAt: c.starts, EndsAt: c.ends,
			})
			if err == nil || errs.Code(err) != errs.InvalidArgument {
				t.Errorf("err = %v (code %v), want InvalidArgument", err, errs.Code(err))
			}
		})
	}
}

func TestGetSlots_UnknownTimezoneIsInvalid(t *testing.T) {
	mon := futureWeekday(t)
	_, err := GetSlots(context.Background(), 1, &GetSlotsParams{
		StartsAt: mon, EndsAt: mon.Add(24 * time.Hour),
		Timezone: "Mars/Olympus_Mons",
	})
	if err == nil || errs.Code(err) != errs.InvalidArgument {
		t.Errorf("err = %v (code %v), want InvalidArgument", err, errs.Code(err))
	}
}
