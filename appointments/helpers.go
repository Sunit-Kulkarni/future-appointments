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

type slotRequest struct {
	RangeStart time.Time
	RangeEnd   time.Time
	TrainerLoc *time.Location
	Booked     map[int64]struct{}
	Now        time.Time
}

func (req slotRequest) isSlotAvailable(start, end time.Time) bool {
	_, taken := req.Booked[start.Unix()]
	inWindow := !start.Before(req.RangeStart) && !end.After(req.RangeEnd)
	future   := !start.Before(req.Now)
	return inWindow && future && !taken
}

func blockFromRow(r db.Availability) (availabilityBlock, bool) {
	if !r.StartTime.Valid || !r.EndTime.Valid {
		return availabilityBlock{}, false
	}
	startSec := r.StartTime.Microseconds / 1_000_000
	endSec := r.EndTime.Microseconds / 1_000_000
	return availabilityBlock{
		startHour: int(startSec / 3600),
		startMin:  int((startSec % 3600) / 60),
		endHour:   int(endSec / 3600),
		endMin:    int((endSec % 3600) / 60),
	}, true
}

// blocksFromRows converts sqlc Availability rows for a single weekday into
// availability blocks. Rows with NULL start/end (unavailable days) are dropped.
func blocksFromRows(rows []db.Availability) []availabilityBlock {
	var out []availabilityBlock
	for _, row := range rows {
		if block, ok := blockFromRow(row); ok {
			out = append(out, block)
		}
	}
	return out
}

// generateSlots emits 30-minute slots for each availability block on each
// weekday inside req.RangeStart..req.RangeEnd, skipping slots that start
// before req.Now or whose start instant is in req.Booked. Slot times are
// constructed directly in req.TrainerLoc via time.Date so DST transitions
// are handled correctly.
//
// The booked set works because every appointment is exactly 30 minutes on
// a :00/:30 boundary (enforced in book.go), so a slot conflicts with a
// booking iff their start instants match — an O(1) hash lookup, not an
// interval intersection. See README "Algorithm & concurrency".
func generateSlots(
	byWeekday map[time.Weekday][]availabilityBlock,
	req slotRequest,
) []bookableSlot {
	var slots []bookableSlot

	startLocal := req.RangeStart.In(req.TrainerLoc)
	endLocal   := req.RangeEnd.In(req.TrainerLoc)

	day  := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, req.TrainerLoc)
	last := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, req.TrainerLoc)

	for !day.After(last) {
		for _, b := range byWeekday[day.Weekday()] {
			slotStart := time.Date(day.Year(), day.Month(), day.Day(), b.startHour, b.startMin, 0, 0, req.TrainerLoc)
			blockEnd  := time.Date(day.Year(), day.Month(), day.Day(), b.endHour, b.endMin, 0, 0, req.TrainerLoc)
			for {
				slotEnd := slotStart.Add(SlotDuration)
				if slotEnd.After(blockEnd) {
					break
				}
				if req.isSlotAvailable(slotStart, slotEnd) {
					slots = append(slots, bookableSlot{Start: slotStart, End: slotEnd})
				}
				slotStart = slotEnd
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return slots
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
