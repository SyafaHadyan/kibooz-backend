// Package season starts the leaderboard over at the beginning of every semester. A child keeps the points of all semesters
// in students.current_points, and the leaderboard ranks students.season_points, which restarts from zero. The final
// standings of the semester that ended are copied to leaderboard_history first.
package season

import (
	"context"
	"errors"
	"fmt"
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

// Interval is how often the job looks at the calendar. The look is a cheap check of the latest semester and of the classes that are left, and it bounds how many points
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
	observe     func(error)
}

func New(db *gorm.DB, cache redis.CacheItf, loc *time.Location, startMonths []int) *Job {
	return &Job{db: db, cache: cache, loc: loc, startMonths: startMonths, now: time.Now}
}

// Observe makes the job call report with the error of every check that the shutdown did not cut short, nil for a success
func (j *Job) Observe(report func(error)) *Job {
	j.observe = report

	return j
}

// WithClock makes the job read the date from now, which is how a test moves time forward
func (j *Job) WithClock(now func() time.Time) *Job {
	j.now = now

	return j
}

// batchSize is how many classes one look at the table reads, so the list that a run keeps in memory stays small
const batchSize = 200

// Run starts a new semester when the calendar says one has begun, and then starts it for every class that has not started it
// yet. Any instance may do both and the work is split into one short transaction for each class, so an instance that stops
// halfway loses nothing, and the next run of any instance continues with the classes that are left. It tells whether it reset
// at least one class. The first run on a database only records the semester that is under way, because the points of every
// class belong to it.
func (j *Job) Run(ctx context.Context) (bool, error) {
	label := Label(j.now(), j.loc, j.startMonths)

	current, err := j.record(ctx, label)
	if err != nil {
		return false, err
	}

	// an instance whose clock is behind finds a later semester recorded, and it must not take the classes back to an older one
	if !current {
		return false, nil
	}

	return j.startClasses(ctx, label)
}

// season is a row of leaderboard_seasons
type season struct {
	Label string `gorm:"primaryKey"`
}

func (season) TableName() string { return "leaderboard_seasons" }

// record notes that the semester has begun and tells whether it is the latest one. The one instance whose insert succeeds is the
// first and any other finds the label taken. A label that sorts before the latest one is never recorded, because the labels are
// the first days of the semesters and so they sort by time, and an instance with a wrong clock would otherwise make an older
// semester the latest one for every instance.
func (j *Job) record(ctx context.Context, label string) (bool, error) {
	current := false

	err := j.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		current = false

		var latest []string

		err := tx.Raw("SELECT label FROM leaderboard_seasons ORDER BY started_at DESC, label DESC LIMIT 1").Scan(&latest).Error
		if err != nil {
			return err
		}

		if len(latest) > 0 && latest[0] == label {
			current = true

			return nil
		}

		if len(latest) > 0 && label < latest[0] {
			return nil
		}

		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&season{Label: label})
		current = inserted.Error == nil && inserted.RowsAffected == 1

		return inserted.Error
	})

	return current, err
}

// startClasses starts the semester for every class that belongs to another one, a batch at a time in the order of the ids. An
// archived class is in too. Its points cannot change, but the semester ends for it as for every class, so its standings are
// saved and a class that is restored later starts from the semester that is under way.
func (j *Job) startClasses(ctx context.Context, label string) (bool, error) {
	reset := false
	after := uuid.Nil

	var failed []error

	for {
		var classIDs []uuid.UUID

		err := j.db.WithContext(ctx).Raw(
			"SELECT c.id FROM classes AS c LEFT JOIN class_seasons AS s ON s.class_id = c.id "+
				"WHERE s.season_label IS DISTINCT FROM ? AND c.id > ? ORDER BY c.id LIMIT ?",
			label, after, batchSize,
		).Scan(&classIDs).Error
		if err != nil {
			return reset, err
		}

		if len(classIDs) == 0 {
			return reset, errors.Join(failed...)
		}

		for _, classID := range classIDs {
			started, err := j.startClass(ctx, classID, label)
			if err != nil {
				// the next class goes on, so one class that fails does not hold back all the classes after it
				if ctx.Err() != nil {
					return reset, err
				}

				failed = append(failed, fmt.Errorf("class %s: %w", classID, err))

				continue
			}

			reset = reset || started
		}

		after = classIDs[len(classIDs)-1]
	}
}

// startClass saves the standings of the semester that the class was in under that label, sets the class back to zero and
// moves it into the new semester, all in one transaction under the ranking lock of the class, like a trash claim. A claim in the
// middle of the reset lands wholly before it or wholly after it. The label is read again under the lock, so a class that
// another instance has started in the meantime is left alone, and its history is never saved twice. A class without a label
// has no semester behind it yet, so it is only marked.
func (j *Job) startClass(ctx context.Context, classID uuid.UUID, label string) (bool, error) {
	started := false

	err := j.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		started = false

		err := ranking.Lock(tx, classID)
		if err != nil {
			return err
		}

		var current []string

		// the read comes after the lock, and a statement of the default isolation level, read committed, sees what the instance that held
		// the lock before committed. A stricter level would read a snapshot from before the wait and miss it.
		err = tx.Raw("SELECT COALESCE(season_label, '') FROM class_seasons WHERE class_id = ?", classID).Scan(&current).Error
		if err != nil {
			return err
		}

		ended := ""
		if len(current) > 0 {
			ended = current[0]
		}

		if ended == label {
			return nil
		}

		if ended != "" {
			started, err = j.saveAndClear(tx, classID, ended)
			if err != nil {
				return err
			}
		}

		return tx.Exec(
			"INSERT INTO class_seasons (class_id, season_label) VALUES (?, ?) "+
				"ON CONFLICT (class_id) DO UPDATE SET season_label = EXCLUDED.season_label",
			classID, label,
		).Error
	})
	if err != nil {
		return false, err
	}

	// the cache is optional, entries also expire by themselves
	if started {
		_ = j.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())
	}

	return started, nil
}

// saveAndClear copies the standings of the semester that ended to leaderboard_history, sets the points of the semester back to
// zero and numbers the class again
func (j *Job) saveAndClear(tx *gorm.DB, classID uuid.UUID, ended string) (bool, error) {
	saved := tx.Exec(
		"INSERT INTO leaderboard_history (class_id, student_id, student_name, season_label, points, rank_position) "+
			"SELECT class_id, id, full_name, ?, season_points, rank_position FROM students "+
			"WHERE deleted_at IS NULL AND season_points > 0 AND class_id = ?",
		ended, classID,
	)
	if saved.Error != nil {
		return false, saved.Error
	}

	cleared := tx.Exec("UPDATE students SET season_points = 0 WHERE season_points <> 0 AND class_id = ?", classID)
	if cleared.Error != nil {
		return false, cleared.Error
	}

	return saved.RowsAffected > 0 || cleared.RowsAffected > 0, ranking.Recompute(tx, classID)
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

	if j.observe != nil && ctx.Err() == nil {
		j.observe(err)
	}

	switch {
	case err != nil && ctx.Err() == nil:
		log.Printf("leaderboard semester check failed %v", err)
	case reset:
		log.Printf("a new semester began, the classes that were not started yet start from zero")
	}
}
