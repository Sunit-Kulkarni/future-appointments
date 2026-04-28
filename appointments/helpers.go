package appointments

import (
	"errors"
	"time"

	"encore.app/appointments/db"

	"github.com/jackc/pgx/v5/pgconn"
)

// availabilityBlock is one continuous bookable window for a trainer on a weekday,
// expressed as wall-clock hours/minutes in the trainer's local timezone.
type availabilityBlock struct {
	startHour, startMin int
	endHour, endMin     int
}

// blocksFromRows converts sqlc Availability rows for a single weekday into
// availability blocks. Rows with NULL start/end (unavailable days) are dropped.
func blocksFromRows(rows []db.Availability) []availabilityBlock {
	var out []availabilityBlock
	for _, r := range rows {
		if !r.StartTime.Valid || !r.EndTime.Valid {
			continue
		}
		startSec := r.StartTime.Microseconds / 1_000_000
		endSec := r.EndTime.Microseconds / 1_000_000
		out = append(out, availabilityBlock{
			startHour: int(startSec / 3600),
			startMin:  int((startSec % 3600) / 60),
			endHour:   int(endSec / 3600),
			endMin:    int((endSec % 3600) / 60),
		})
	}
	return out
}

// generateSlots emits 30-minute slots between dayStart and dayEnd (inclusive
// of dayStart-instant range) for each block on each weekday inside the range.
// All slot times are constructed directly in loc via time.Date so DST
// transitions are handled correctly.
func generateSlots(rangeStart, rangeEnd time.Time, byWeekday map[time.Weekday][]availabilityBlock, loc *time.Location) []bookableSlot {
	var slots []bookableSlot

	// Iterate by calendar day in loc.
	startLocal := rangeStart.In(loc)
	endLocal := rangeEnd.In(loc)

	day := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	last := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, loc)

	for !day.After(last) {
		blocks := byWeekday[day.Weekday()]
		for _, b := range blocks {
			slotStart := time.Date(day.Year(), day.Month(), day.Day(), b.startHour, b.startMin, 0, 0, loc)
			blockEnd := time.Date(day.Year(), day.Month(), day.Day(), b.endHour, b.endMin, 0, 0, loc)
			for {
				slotEnd := slotStart.Add(SlotDuration)
				if slotEnd.After(blockEnd) {
					break
				}
				// Clip to the requested instant range.
				if !slotStart.Before(rangeStart) && !slotEnd.After(rangeEnd) {
					slots = append(slots, bookableSlot{Start: slotStart, End: slotEnd})
				}
				slotStart = slotEnd
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return slots
}

// filterBookedSlots removes any slot whose [start,end) overlaps a booked
// appointment using the standard overlap predicate.
func filterBookedSlots(slots []bookableSlot, booked []db.Appointment) []bookableSlot {
	if len(booked) == 0 {
		return slots
	}
	out := slots[:0]
	for _, s := range slots {
		conflict := false
		for _, b := range booked {
			if b.StartsAt.Time.Before(s.End) && b.EndsAt.Time.After(s.Start) {
				conflict = true
				break
			}
		}
		if !conflict {
			out = append(out, s)
		}
	}
	return out
}

// filterPast removes slots that start before now.
func filterPast(slots []bookableSlot, now time.Time) []bookableSlot {
	out := slots[:0]
	for _, s := range slots {
		if !s.Start.Before(now) {
			out = append(out, s)
		}
	}
	return out
}

// withinAvailability reports whether [localStart,localEnd] sits inside one
// of the blocks for that weekday.
func withinAvailability(localStart, localEnd time.Time, blocks []availabilityBlock) bool {
	loc := localStart.Location()
	y, m, d := localStart.Date()
	for _, b := range blocks {
		blockStart := time.Date(y, m, d, b.startHour, b.startMin, 0, 0, loc)
		blockEnd := time.Date(y, m, d, b.endHour, b.endMin, 0, 0, loc)
		if !localStart.Before(blockStart) && !localEnd.After(blockEnd) {
			return true
		}
	}
	return false
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
