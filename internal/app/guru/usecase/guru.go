// Package usecase holds the teacher portal business rules
package usecase

import (
	"context"
	"regexp"
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
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

const (
	RangeWeekly  = "weekly"
	RangeMonthly = "monthly"
)

// phonePattern accepts 6 to 30 characters of digits with an optional leading plus, and spaces, dashes and brackets between them
var phonePattern = regexp.MustCompile(`^\+?\(?[0-9][0-9 ()-]{4,26}[0-9]$`)

type GuruUseCaseItf interface {
	Dashboard(ctx context.Context, userID uuid.UUID, classID *uuid.UUID) (dto.GuruDashboardResponse, error)
	LogMood(ctx context.Context, userID uuid.UUID, req dto.LogMoodRequest) (dto.LogMoodResponse, error)
	MoodAnalytics(ctx context.Context, userID uuid.UUID, classID *uuid.UUID, rangeName string) (dto.MoodAnalyticsResponse, error)
	ListClasses(ctx context.Context, userID uuid.UUID, page pagination.Params) (dto.ClassList, error)
	Class(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassDetail, error)
	CreateClass(ctx context.Context, userID uuid.UUID, req dto.CreateClassRequest) (dto.ClassSummary, error)
	// RotateJoinCode replaces the join code of a class the teacher teaches, and parents can no longer join with the old one
	RotateJoinCode(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.JoinCodeResponse, error)
	Profile(ctx context.Context, userID uuid.UUID) (dto.GuruProfile, error)
	ProfileDetail(ctx context.Context, userID uuid.UUID) (dto.GuruProfileDetail, error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, req dto.UpdateGuruProfileRequest) (dto.GuruProfileDetail, error)
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
		return dto.LogMoodResponse{}, apperror.Validation(map[string]string{"confidenceScore": "is required for the AI_CAMERA source"})
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
		RecordedAt:       u.now().UTC().Truncate(time.Microsecond),
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
		return dto.MoodAnalyticsResponse{}, apperror.Validation(map[string]string{"range": "must be one of weekly monthly"})
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

func (u *GuruUseCase) ListClasses(ctx context.Context, userID uuid.UUID, page pagination.Params) (dto.ClassList, error) {
	guru, err := u.findGuru(ctx, userID)
	if err != nil {
		return dto.ClassList{}, err
	}

	rows, total, err := u.repo.ListTaughtClasses(ctx, guru.ID, page.Limit, page.Offset())
	if err != nil {
		return dto.ClassList{}, apperror.Internal(err)
	}

	res := dto.ClassList{
		Classes:  make([]dto.ClassSummary, 0, len(rows)),
		PageInfo: dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range rows {
		res.Classes = append(res.Classes, classSummary(&rows[i]))
	}

	return res, nil
}

func (u *GuruUseCase) Class(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassDetail, error) {
	guru, err := u.findGuru(ctx, userID)
	if err != nil {
		return dto.ClassDetail{}, err
	}

	row, err := u.repo.FindClassRow(ctx, guru.ID, classID)
	if err != nil {
		return dto.ClassDetail{}, apperror.Internal(err)
	}

	if row == nil {
		return dto.ClassDetail{}, apperror.ErrClassNotFound
	}

	videos, threads, err := u.repo.CountClassContent(ctx, classID)
	if err != nil {
		return dto.ClassDetail{}, apperror.Internal(err)
	}

	return dto.ClassDetail{ClassSummary: classSummary(row), TotalVideos: videos, TotalThreads: threads}, nil
}

func (u *GuruUseCase) RotateJoinCode(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.JoinCodeResponse, error) {
	_, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return dto.JoinCodeResponse{}, err
	}

	code, err := u.repo.RotateJoinCode(ctx, class.ID)
	if err != nil {
		return dto.JoinCodeResponse{}, apperror.Internal(err)
	}

	return dto.JoinCodeResponse{JoinCode: code}, nil
}

func (u *GuruUseCase) CreateClass(ctx context.Context, userID uuid.UUID, req dto.CreateClassRequest) (dto.ClassSummary, error) {
	guru, err := u.findGuru(ctx, userID)
	if err != nil {
		return dto.ClassSummary{}, err
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return dto.ClassSummary{}, apperror.Validation(map[string]string{"name": "is required"})
	}

	class := &entity.Class{
		ID:           uuid.New(),
		SchoolName:   firstNonEmpty(req.SchoolName, guru.SchoolName),
		Name:         name,
		GradeLevel:   firstNonEmpty(req.GradeLevel, constants.DefaultGradeLevel),
		AcademicYear: firstNonEmpty(req.AcademicYear, constants.DefaultAcademicYr),
	}

	err = u.repo.CreateClass(ctx, guru.ID, class)
	if err != nil {
		return dto.ClassSummary{}, apperror.Internal(err)
	}

	return classSummary(&repository.ClassRow{Class: *class}), nil
}

func (u *GuruUseCase) Profile(ctx context.Context, userID uuid.UUID) (dto.GuruProfile, error) {
	guru, err := u.findGuru(ctx, userID)
	if err != nil {
		return dto.GuruProfile{}, err
	}

	return profileResponse(guru), nil
}

func (u *GuruUseCase) ProfileDetail(ctx context.Context, userID uuid.UUID) (dto.GuruProfileDetail, error) {
	guru, err := u.findGuru(ctx, userID)
	if err != nil {
		return dto.GuruProfileDetail{}, err
	}

	return detailResponse(guru), nil
}

func (u *GuruUseCase) UpdateProfile(ctx context.Context, userID uuid.UUID, req dto.UpdateGuruProfileRequest) (dto.GuruProfileDetail, error) {
	guru, err := u.findGuru(ctx, userID)
	if err != nil {
		return dto.GuruProfileDetail{}, err
	}

	update, err := profileUpdate(req)
	if err != nil {
		return dto.GuruProfileDetail{}, err
	}

	err = u.repo.UpdateProfile(ctx, userID, guru.ID, update)
	if err != nil {
		return dto.GuruProfileDetail{}, apperror.Internal(err)
	}

	return u.ProfileDetail(ctx, userID)
}

func (u *GuruUseCase) findGuru(ctx context.Context, userID uuid.UUID) (*repository.GuruRow, error) {
	guru, err := u.repo.FindGuruByUserID(ctx, userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if guru == nil {
		return nil, apperror.ErrProfileNotFound
	}

	return guru, nil
}

// profileUpdate checks the changed fields. An empty text clears a field and a missing one is left alone.
func profileUpdate(req dto.UpdateGuruProfileRequest) (repository.ProfileUpdate, error) {
	var update repository.ProfileUpdate

	if req.PhoneNumber == nil && req.Address == nil {
		return update, apperror.Validation(map[string]string{"body": "provide phoneNumber or address"})
	}

	if req.PhoneNumber != nil {
		phone := strings.TrimSpace(*req.PhoneNumber)
		if phone != "" && !phonePattern.MatchString(phone) {
			return update, apperror.Validation(map[string]string{"phoneNumber": "must be a valid phone number"})
		}

		update.SetPhone, update.Phone = true, optionalText(phone)
	}

	if req.Address != nil {
		update.SetAddress, update.Address = true, optionalText(*req.Address)
	}

	return update, nil
}

func classSummary(row *repository.ClassRow) dto.ClassSummary {
	return dto.ClassSummary{
		ID:            row.ID,
		Name:          row.Name,
		GradeLevel:    row.GradeLevel,
		SchoolName:    row.SchoolName,
		AcademicYear:  row.AcademicYear,
		JoinCode:      row.JoinCode,
		TotalStudents: row.TotalStudents,
		CreatedAt:     row.CreatedAt.UTC(),
	}
}

func profileResponse(guru *repository.GuruRow) dto.GuruProfile {
	return dto.GuruProfile{
		ID:         guru.UserID,
		FullName:   guru.FullName,
		NIP:        guru.NIP,
		SchoolName: guru.SchoolName,
		AvatarURL:  guru.AvatarURL,
	}
}

func detailResponse(guru *repository.GuruRow) dto.GuruProfileDetail {
	return dto.GuruProfileDetail{
		GuruProfile: profileResponse(guru),
		Email:       guru.Email,
		PhoneNumber: guru.PhoneNumber,
		Address:     guru.Address,
		JoinedAt:    guru.JoinedAt.UTC(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func optionalText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return &value
}
