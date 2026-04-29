package appointments

import (
	"errors"
	"testing"
	"time"

	"encore.app/appointments/db"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// pgTime constructs a pgtype.Time from hour/minute (the only fields that
// blocksFromRows reads), matching what sqlc would yield for a TIME column.
func pgTime(h, m int) pgtype.Time {
	return pgtype.Time{Microseconds: int64((h*3600 + m*60) * 1_000_000), Valid: true}
}

func TestBlocksFromRows(t *testing.T) {
	t.Parallel()

	rows := []db.Availability{
		// Unavailable day — should be skipped.
		{StartTime: pgtype.Time{Valid: false}, EndTime: pgtype.Time{Valid: false}},
		// Standard 8–17 block.
		{StartTime: pgTime(8, 0), EndTime: pgTime(17, 0)},
		// Split-day: 9–12 then 13–17 (two rows, same trainer/weekday).
		{StartTime: pgTime(9, 0), EndTime: pgTime(12, 0)},
		{StartTime: pgTime(13, 0), EndTime: pgTime(17, 0)},
	}

	got := blocksFromRows(rows)
	if len(got) != 3 {
		t.Fatalf("expected 3 blocks (NULL row dropped), got %d", len(got))
	}
	if got[0].startHour != 8 || got[0].endHour != 17 {
		t.Errorf("first block wrong: %+v", got[0])
	}
	if got[1].startHour != 9 || got[1].endHour != 12 {
		t.Errorf("second block wrong: %+v", got[1])
	}
}

func TestBlockFromRow(t *testing.T) {
	t.Parallel()

	t.Run("valid 8-17 block", func(t *testing.T) {
		row := db.Availability{StartTime: pgTime(8, 0), EndTime: pgTime(17, 0)}
		got, ok := blockFromRow(row)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.startHour != 8 || got.startMin != 0 || got.endHour != 17 || got.endMin != 0 {
			t.Errorf("unexpected block: %+v", got)
		}
	})

	t.Run("NULL start_time", func(t *testing.T) {
		row := db.Availability{StartTime: pgtype.Time{Valid: false}, EndTime: pgTime(17, 0)}
		_, ok := blockFromRow(row)
		if ok {
			t.Fatal("expected ok=false for NULL start_time")
		}
	})

	t.Run("NULL end_time", func(t *testing.T) {
		row := db.Availability{StartTime: pgTime(8, 0), EndTime: pgtype.Time{Valid: false}}
		_, ok := blockFromRow(row)
		if ok {
			t.Fatal("expected ok=false for NULL end_time")
		}
	})

	t.Run("split day 9-12 block", func(t *testing.T) {
		row := db.Availability{StartTime: pgTime(9, 0), EndTime: pgTime(12, 0)}
		got, ok := blockFromRow(row)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.startHour != 9 || got.startMin != 0 || got.endHour != 12 || got.endMin != 0 {
			t.Errorf("unexpected block: %+v", got)
		}
	})
}

func TestWithinAvailability(t *testing.T) {
	t.Parallel()
	loc, _ := time.LoadLocation("America/Los_Angeles")
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, loc) // Monday
	at := func(h, m int) time.Time { return time.Date(2026, 5, 4, h, m, 0, 0, loc) }

	blocks := []availabilityBlock{
		{startHour: 8, startMin: 0, endHour: 17, endMin: 0},
	}

	cases := []struct {
		name           string
		start, end     time.Time
		blocks         []availabilityBlock
		wantWithin     bool
	}{
		{"inside block", at(9, 0), at(9, 30), blocks, true},
		{"exactly at start", at(8, 0), at(8, 30), blocks, true},
		{"exactly at end", at(16, 30), at(17, 0), blocks, true},
		{"before start", at(7, 30), at(8, 0), blocks, false},
		{"past end", at(17, 0), at(17, 30), blocks, false},
		{"empty blocks", at(9, 0), at(9, 30), nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withinAvailability(c.start, c.end, c.blocks); got != c.wantWithin {
				t.Errorf("withinAvailability(%v..%v) = %v, want %v", c.start, c.end, got, c.wantWithin)
			}
		})
	}

	_ = day // silence unused if cases ever shrink
}

func TestGenerateSlots(t *testing.T) {
	t.Parallel()
	loc, _ := time.LoadLocation("America/Los_Angeles")
	mon := time.Date(2026, 5, 4, 0, 0, 0, 0, loc) // Monday
	sat := time.Date(2026, 5, 9, 0, 0, 0, 0, loc) // Saturday

	weekdays := map[time.Weekday][]availabilityBlock{
		time.Monday: {{startHour: 8, endHour: 17}},
	}
	pastNow := time.Date(2000, 1, 1, 0, 0, 0, 0, loc) // far in the past, no past-filter effect

	t.Run("monday produces 18 slots", func(t *testing.T) {
		got := generateSlots(mon, mon.Add(24*time.Hour), weekdays, loc, nil, pastNow)
		if len(got) != 18 {
			t.Fatalf("want 18 slots, got %d", len(got))
		}
		if !got[0].Start.Equal(time.Date(2026, 5, 4, 8, 0, 0, 0, loc)) {
			t.Errorf("first slot wrong: %v", got[0].Start)
		}
		if !got[17].End.Equal(time.Date(2026, 5, 4, 17, 0, 0, 0, loc)) {
			t.Errorf("last slot end wrong: %v", got[17].End)
		}
	})

	t.Run("saturday produces 0 slots", func(t *testing.T) {
		got := generateSlots(sat, sat.Add(24*time.Hour), weekdays, loc, nil, pastNow)
		if len(got) != 0 {
			t.Fatalf("want 0 slots on Saturday, got %d", len(got))
		}
	})

	t.Run("booked set excludes matching slot", func(t *testing.T) {
		nineAM := time.Date(2026, 5, 4, 9, 0, 0, 0, loc)
		booked := map[int64]struct{}{nineAM.Unix(): {}}
		got := generateSlots(mon, mon.Add(24*time.Hour), weekdays, loc, booked, pastNow)
		if len(got) != 17 {
			t.Fatalf("want 17 slots (one booked), got %d", len(got))
		}
		for _, s := range got {
			if s.Start.Equal(nineAM) {
				t.Errorf("booked slot %v leaked through", s.Start)
			}
		}
	})

	t.Run("future cutoff drops past slots", func(t *testing.T) {
		// Cutoff at 13:00 PT — slots starting before 13:00 are dropped.
		now := time.Date(2026, 5, 4, 13, 0, 0, 0, loc)
		got := generateSlots(mon, mon.Add(24*time.Hour), weekdays, loc, nil, now)
		if len(got) != 8 { // 13:00..16:30 inclusive = 8 slots
			t.Fatalf("want 8 future slots, got %d", len(got))
		}
		if !got[0].Start.Equal(now) {
			t.Errorf("first future slot %v != now %v", got[0].Start, now)
		}
	})

	t.Run("window clipping drops slots outside range", func(t *testing.T) {
		// Window: 10:00..12:00 PT — only 10:00, 10:30, 11:00, 11:30 fit.
		start := time.Date(2026, 5, 4, 10, 0, 0, 0, loc)
		end := time.Date(2026, 5, 4, 12, 0, 0, 0, loc)
		got := generateSlots(start, end, weekdays, loc, nil, pastNow)
		if len(got) != 4 {
			t.Fatalf("want 4 clipped slots, got %d", len(got))
		}
	})
}

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unique violation 23505", &pgconn.PgError{Code: "23505"}, true},
		{"foreign-key violation 23503", &pgconn.PgError{Code: "23503"}, false},
		{"check violation 23514", &pgconn.PgError{Code: "23514"}, false},
		{"non-pg error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isUniqueViolation(c.err); got != c.want {
				t.Errorf("isUniqueViolation(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
