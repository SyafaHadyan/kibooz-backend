// Package season starts the leaderboard over at the beginning of every semester. A child keeps the points of all semesters
// in students.current_points, and the leaderboard ranks students.season_points, which restarts from zero. The final
// standings of the semester that ended are copied to leaderboard_history first.
package season

import (
	"context"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

// Interval is how often the job looks at the calendar. The look is a single cheap query, and it bounds how many points
// earned right after a semester begins are still counted in the semester that ended. A semester that begins while the API is
// down is picked up by the first look after it starts.
const Interval = time.Minute

// Label names the semester that holds t by the day it began, such as "2026-07" for a semester that began on 1 July 2026.
// startMonths are the months in which a semester begins, in any order, and a semester begins on the first day of its month in loc.
func Label(t time.Time, loc *time.Location, startMonths []int) string {
	local := t.In(loc)
	months := slices.Sorted(slices.Values(startMonths))

	year, month := local.Year(), int(local.Month())

	// the semester that began last is the largest start month that is not after the current one, or the largest of last year
	begun := -1

	for _, m := range months {
		if m <= month {
			begun = m
		}
	}

	if begun == -1 {
		year--
		begun = months[len(months)-1]
	}

	return time.Date(year, time.Month(begun), 1, 0, 0, 0, 0, loc).Format("2006-01")
}

// Job resets the leaderboard when a new semester has begun
type Job struct {
	db          *gorm.DB
	cache       redis.CacheItf
	loc         *time.Location
	startMonths []int
	now         func() time.Time
}

func New(db *gorm.DB, cache redis.CacheItf, loc *time.Location, startMonths []int) *Job {
	return &Job{db: db, cache: cache, loc: loc, startMonths: startMonths, now: time.Now}
}

// WithClock makes the job read the date from now, which is how a test moves time forward
func (j *Job) WithClock(now func() time.Time) *Job {
	j.now = now

	return j
}

// Run starts a new semester when the calendar says one has begun and no instance has done it yet. The first run on a
// database only records the semester that is under way, because its points belong to it. It tells whether it reset.
func (j *Job) Run(ctx context.Context) (bool, error) {
	label := Label(j.now(), j.loc, j.startMonths)

	var classIDs []uuid.UUID

	reset := false

	err := j.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		reset = false
		classIDs = nil

		var latest []string

		err := tx.Raw("SELECT label FROM leaderboard_seasons ORDER BY started_at DESC, label DESC LIMIT 1").Scan(&latest).Error
		if err != nil {
			return err
		}

		if len(latest) > 0 && latest[0] == label {
			return nil
		}

		// the one instance whose insert succeeds does the reset, any other that is late finds the label taken
		started := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&season{Label: label})
		if started.Error != nil {
			return started.Error
		}

		if started.RowsAffected == 0 || len(latest) == 0 {
			return nil
		}

		classIDs, err = j.startSemester(tx, latest[0])
		reset = err == nil

		return err
	})
	if err != nil {
		return false, err
	}

	// the cache is optional, entries also expire by themselves
	for _, classID := range classIDs {
		_ = j.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())
	}

	return reset, nil
}

// season is a row of leaderboard_seasons
type season struct {
	Label string `gorm:"primaryKey"`
}

func (season) TableName() string { return "leaderboard_seasons" }

// startSemester saves the standings of the semester that ended under its label, sets every class back to zero and
// returns the classes that were touched. The classes are locked in one fixed order, like a trash claim locks its class, so
// a claim in the middle of the reset lands wholly before it or wholly after it.
func (j *Job) startSemester(tx *gorm.DB, ended string) ([]uuid.UUID, error) {
	var classIDs []uuid.UUID

	err := tx.Raw("SELECT id FROM classes ORDER BY id").Scan(&classIDs).Error
	if err != nil {
		return nil, err
	}

	for _, classID := range classIDs {
		err = ranking.Lock(tx, classID)
		if err != nil {
			return nil, err
		}
	}

	err = tx.Exec(
		"INSERT INTO leaderboard_history (class_id, student_id, student_name, season_label, points, rank_position) "+
			"SELECT class_id, id, full_name, ?, season_points, rank_position FROM students "+
			"WHERE deleted_at IS NULL AND season_points > 0",
		ended,
	).Error
	if err != nil {
		return nil, err
	}

	err = tx.Exec("UPDATE students SET season_points = 0 WHERE season_points <> 0").Error
	if err != nil {
		return nil, err
	}

	for _, classID := range classIDs {
		err = ranking.Recompute(tx, classID)
		if err != nil {
			return nil, err
		}
	}

	return classIDs, nil
}

// Start looks at the calendar once now and then once per interval until the returned stop function is called, which
// waits for a run that is under way. A zero or negative interval disables it.
func (j *Job) Start(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			j.runAndLog(ctx)

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})

	return func() {
		cancel()
		wg.Wait()
	}
}

func (j *Job) runAndLog(ctx context.Context) {
	reset, err := j.Run(ctx)

	switch {
	case err != nil && ctx.Err() == nil:
		log.Printf("leaderboard semester check failed %v", err)
	case reset:
		log.Printf("a new semester began, the leaderboard of every class starts from zero")
	}
}
