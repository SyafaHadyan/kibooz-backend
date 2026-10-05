// Package usecase holds the teacher portal business rules
package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/clock"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

const (
	RangeWeekly  = "weekly"
	RangeMonthly = "monthly"
)

type GuruUseCaseItf interface {
	Dashboard(ctx context.Context, userID uuid.UUID, classID *uuid.UUID) (dto.GuruDashboardResponse, error)
	LogMood(ctx context.Context, userID uuid.UUID, req dto.LogMoodRequest) (dto.LogMoodResponse, error)
	MoodAnalytics(ctx context.Context, userID uuid.UUID, classID *uuid.UUID, rangeName string) (dto.MoodAnalyticsResponse, error)
}

type GuruUseCase struct {
	repo repository.GuruDBItf
	cfg  *env.Env
	now  func() time.Time
}

func NewGuruUseCase(repo repository.GuruDBItf, cfg *env.Env) GuruUseCaseItf {
	return &GuruUseCase{repo: repo, cfg: cfg, now: time.Now}
}

func (u *GuruUseCase) Dashboard(ctx context.Context, userID uuid.UUID, classID *uuid.UUID) (dto.GuruDashboardResponse, error) {
	guru, class, err := u.resolveClass(ctx, userID, classID)
	if err != nil {
		return dto.GuruDashboardResponse{}, err
	}

	total, err := u.repo.CountStudents(ctx, class.ID)
	if err != nil {
		return dto.GuruDashboardResponse{}, apperror.Internal(err)
	}

	loc := u.cfg.Location()
	from, to := clock.DayBounds(u.now(), loc)

	records, err := u.repo.ListMoodRecords(ctx, class.ID, from, to)
	if err != nil {
		return dto.GuruDashboardResponse{}, apperror.Internal(err)
	}

	today := latestPerStudentPerDay(records, loc)
	counts := countMoods(today)

	return dto.GuruDashboardResponse{
		Teacher: dto.GuruTeacher{
			FullName:  guru.FullName,
			NIP:       guru.NIP,
			AvatarURL: guru.AvatarURL,
		},
		ClassOverview: dto.ClassOverview{
			ClassID:         class.ID,
			ClassName:       class.GradeLevel + " (" + class.Name + ")",
			JoinCode:        class.JoinCode,
			TotalStudents:   total,
			PresentStudents: len(today),
			DominantMood:    dominantMood(counts),
		},
		DailyMoodDistribution: dto.DailyMoodDistribution{
			Senang:  counts[constants.MoodSenang],
			Sedih:   counts[constants.MoodSedih],
			Marah:   counts[constants.MoodMarah],
			Bingung: counts[constants.MoodBingung],
		},
	}, nil
}

func (u *GuruUseCase) LogMood(ctx context.Context, userID uuid.UUID, req dto.LogMoodRequest) (dto.LogMoodResponse, error) {
	guru, err := u.repo.FindGuruByUserID(ctx, userID)
	if err != nil {
		return dto.LogMoodResponse{}, apperror.Internal(err)
	}

	if guru == nil {
		return dto.LogMoodResponse{}, apperror.ErrProfileNotFound
	}

	source := req.Source
	if source == "" {
		source = constants.SourceManualInput
	}

	confidence := float32(1)

	switch {
	case req.ConfidenceScore != nil:
		confidence = *req.ConfidenceScore
	case source == constants.SourceAICamera:
		return dto.LogMoodResponse{}, apperror.Validation(map[string]string{"confidenceScore": "wajib diisi untuk sumber AI_CAMERA"})
	}

	student, err := u.repo.FindStudent(ctx, req.StudentID)
	if err != nil {
		return dto.LogMoodResponse{}, apperror.Internal(err)
	}

	if student == nil {
		return dto.LogMoodResponse{}, apperror.ErrStudentNotFound
	}

	teaches, err := u.repo.TeachesClass(ctx, guru.ID, student.ClassID)
	if err != nil {
		return dto.LogMoodResponse{}, apperror.Internal(err)
	}

	if !teaches {
		return dto.LogMoodResponse{}, apperror.ErrForbidden
	}

	moodLog := &entity.MoodLog{
		ID:               uuid.New(),
		StudentID:        student.ID,
		RecordedByGuruID: guru.ID,
		MoodType:         req.MoodType,
		ConfidenceScore:  confidence,
		Source:           source,
		RecordedAt:       u.now().UTC().Truncate(time.Second),
	}

	if notes := strings.TrimSpace(req.Notes); notes != "" {
		moodLog.Notes = &notes
	}

	err = u.repo.CreateMoodLog(ctx, moodLog)
	if err != nil {
		return dto.LogMoodResponse{}, apperror.Internal(err)
	}

	return dto.LogMoodResponse{LogID: moodLog.ID, RecordedAt: moodLog.RecordedAt}, nil
}

func (u *GuruUseCase) MoodAnalytics(
	ctx context.Context, userID uuid.UUID, classID *uuid.UUID, rangeName string,
) (dto.MoodAnalyticsResponse, error) {
	if rangeName == "" {
		rangeName = RangeWeekly
	}

	if rangeName != RangeWeekly && rangeName != RangeMonthly {
		return dto.MoodAnalyticsResponse{}, apperror.Validation(map[string]string{"range": "harus salah satu dari weekly monthly"})
	}

	_, class, err := u.resolveClass(ctx, userID, classID)
	if err != nil {
		return dto.MoodAnalyticsResponse{}, err
	}

	loc := u.cfg.Location()
	now := u.now()
	weekStart, weekEnd := clock.WeekBounds(now, loc)

	from, to := weekStart, weekEnd
	if rangeName == RangeMonthly {
		from, to = clock.MonthBounds(now, loc)
	}

	// the query window must also cover the current week so the trend works around month borders
	queryFrom, queryTo := minTime(from, weekStart), maxTime(to, weekEnd)

	records, err := u.repo.ListMoodRecords(ctx, class.ID, queryFrom, queryTo)
	if err != nil {
		return dto.MoodAnalyticsResponse{}, apperror.Internal(err)
	}

	all := latestPerStudentPerDay(records, loc)

	inRange := make([]dayMood, 0, len(all))
	fromDay, toDay := from.In(loc).Format(time.DateOnly), to.In(loc).Format(time.DateOnly)

	for _, item := range all {
		if item.Day >= fromDay && item.Day < toDay {
			inRange = append(inRange, item)
		}
	}

	counts := countMoods(inRange)

	res := dto.MoodAnalyticsResponse{
		DonutSummary: donutSlices(counts),
		WeeklyTrend:  weeklyTrend(all, weekStart, loc),
	}

	if rangeName == RangeMonthly {
		res.MonthlyDistribution = make([]dto.MoodCount, 0, len(constants.Moods))
		for _, mood := range constants.Moods {
			res.MonthlyDistribution = append(res.MonthlyDistribution, dto.MoodCount{Mood: mood, Count: counts[mood]})
		}
	}

	return res, nil
}

func (u *GuruUseCase) resolveClass(
	ctx context.Context, userID uuid.UUID, classID *uuid.UUID,
) (*repository.GuruRow, *entity.Class, error) {
	guru, err := u.repo.FindGuruByUserID(ctx, userID)
	if err != nil {
		return nil, nil, apperror.Internal(err)
	}

	if guru == nil {
		return nil, nil, apperror.ErrProfileNotFound
	}

	class, err := u.repo.FindTaughtClass(ctx, guru.ID, classID)
	if err != nil {
		return nil, nil, apperror.Internal(err)
	}

	if class == nil {
		return nil, nil, apperror.ErrClassNotFound
	}

	return guru, class, nil
}

func minTime(a time.Time, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}

func maxTime(a time.Time, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}
