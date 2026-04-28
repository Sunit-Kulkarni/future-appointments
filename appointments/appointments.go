// Service appointments owns trainer availability, slot generation, and bookings.
package appointments

import (
	"context"
	_ "embed"
	"encoding/json"
	"time"
	_ "time/tzdata" // embed IANA tz database so Alpine containers work

	"encore.app/appointments/db"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"encore.dev"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
)

const (
	SlotDuration  = 30 * time.Minute
	BusinessStart = 8
	BusinessEnd   = 17
)

var (
	appointmentsDB = sqldb.NewDatabase("appointments", sqldb.DatabaseConfig{
		Migrations: "./db/migrations",
	})

	pgxdb = sqldb.Driver[*pgxpool.Pool](appointmentsDB)
	query = db.New(pgxdb)
)

//go:embed db/fixtures.sql
var fixturesSQL string

//go:embed appointments.json
var appointmentsJSON []byte

// seedAppointment matches the on-disk shape of appointments.json (which uses
// started_at/ended_at), independent of the API field names (starts_at/ends_at).
type seedAppointment struct {
	ID        int64  `json:"id"`
	TrainerID int32  `json:"trainer_id"`
	UserID    int32  `json:"user_id"`
	StartsAt  string `json:"started_at"`
	EndsAt    string `json:"ended_at"`
}

func init() {
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return
	}
	go seed()
}

func seed() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := appointmentsDB.Exec(ctx, fixturesSQL); err != nil {
		rlog.Error("seed: failed to load fixtures.sql", "err", err)
		return
	}

	var records []seedAppointment
	if err := json.Unmarshal(appointmentsJSON, &records); err != nil {
		rlog.Error("seed: failed to parse appointments.json", "err", err)
		return
	}

	for _, r := range records {
		started, err := time.Parse(time.RFC3339, r.StartsAt)
		if err != nil {
			rlog.Error("seed: bad starts_at", "id", r.ID, "err", err)
			continue
		}
		ended, err := time.Parse(time.RFC3339, r.EndsAt)
		if err != nil {
			rlog.Error("seed: bad ends_at", "id", r.ID, "err", err)
			continue
		}
		_, err = appointmentsDB.Exec(ctx, `
			INSERT INTO appointments (trainer_id, user_id, starts_at, ends_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (trainer_id, starts_at) DO NOTHING
		`,
			r.TrainerID, r.UserID,
			pgtype.Timestamptz{Time: started, Valid: true},
			pgtype.Timestamptz{Time: ended, Valid: true},
		)
		if err != nil {
			rlog.Error("seed: insert failed", "id", r.ID, "err", err)
		}
	}
	rlog.Info("seed: complete", "appointments", len(records))
}
