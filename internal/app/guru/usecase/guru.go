// Package usecase holds the teacher portal business rules
package usecase

import (
	"context"
	"errors"
	"strconv"
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
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

const (
	RangeWeekly  = "weekly"
	RangeMonthly = "monthly"
)

// phonePattern is the pattern that every phone number of a profile has to match
var phonePattern = validation.PhonePattern

type GuruUseCaseItf interface {
	Dashboard(ctx context.Context, userID uuid.UUID, classID *uuid.UUID) (dto.GuruDashboardResponse, error)
	LogMood(ctx context.Context, userID uuid.UUID, req dto.LogMoodRequest) (dto.LogMoodResponse, error)
	// LogMoodBatch stores one mood log per entry for students of a class the teacher teaches, all or none
	LogMoodBatch(ctx context.Context, userID uuid.UUID, req dto.LogMoodBatchRequest) (dto.LogMoodBatchResponse, error)
	MoodAnalytics(ctx context.Context, userID uuid.UUID, classID *uuid.UUID, rangeName string) (dto.MoodAnalyticsResponse, error)
	ListClasses(ctx context.Context, userID uuid.UUID, page pagination.Params) (dto.ClassList, error)
	Class(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassDetail, error)
	CreateClass(ctx context.Context, userID uuid.UUID, req dto.CreateClassRequest) (dto.ClassSummary, error)
	// RotateJoinCode replaces the join code of a class the teacher teaches, and parents can no longer join with the old one
	RotateJoinCode(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.JoinCodeResponse, error)
	// SetJoining lets a teacher of the class open or close it for new children, the join code stays the same
	SetJoining(ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.SetJoiningRequest) (dto.JoiningResponse, error)
	// RemoveStudent lets a teacher of the class take a child out of it, which hides the child like a deleted account does
	RemoveStudent(ctx context.Context, userID uuid.UUID, classID uuid.UUID, studentID uuid.UUID) error
	// MoveRequests lists the requests to move a child into the class that wait for a teacher
	MoveRequests(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassMoveRequestList, error)
	// AcceptMove moves the child of a request into the class, where it starts from zero, and clears the leaderboards of both classes
	AcceptMove(ctx context.Context, userID uuid.UUID, classID uuid.UUID, requestID uuid.UUID) error
	// ArchiveClass archives a class of the caller, who has to be its owner, which makes the class read only
	ArchiveClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassSummary, error)
	// RestoreClass makes an archived class of the caller writable again, with a new join code
	RestoreClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassSummary, error)
	// StudentHistory shows what a child of the class did in the classes it left, read only
	StudentHistory(ctx context.Context, userID uuid.UUID, classID uuid.UUID, studentID uuid.UUID) (dto.StudentHistory, error)
	// RejectMove turns down a request to move a child into the class
	RejectMove(ctx context.Context, userID uuid.UUID, classID uuid.UUID, requestID uuid.UUID) error
	// Teachers lists the teachers of a class the caller teaches
	Teachers(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.TeacherList, error)
	// AddTeacher lets a teacher of the class add another registered teacher by email, who then sees the whole class
	AddTeacher(ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.AddTeacherRequest) (dto.Teacher, error)
	// RemoveTeacher lets the owner of the class take any teacher off it and any teacher leave it, except the last one
	RemoveTeacher(ctx context.Context, userID uuid.UUID, classID uuid.UUID, teacherID uuid.UUID) error
	// TransferOwnership lets the owner of the class hand it to another teacher of the class
	TransferOwnership(ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.TransferOwnershipRequest) (dto.Teacher, error)
	Profile(ctx context.Context, userID uuid.UUID) (dto.GuruProfile, error)
	ProfileDetail(ctx context.Context, userID uuid.UUID) (dto.GuruProfileDetail, error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, req dto.UpdateGuruProfileRequest) (dto.GuruProfileDetail, error)
}

type GuruUseCase struct {
	repo  repository.GuruDBItf
	cache redis.CacheItf
	cfg   *env.Env
	now   func() time.Time
}

func NewGuruUseCase(repo repository.GuruDBItf, cache redis.CacheItf, cfg *env.Env) GuruUseCaseItf {
	return &GuruUseCase{repo: repo, cache: cache, cfg: cfg, now: time.Now}
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

	records, err := u.repo.ListMoodRecords(ctx, class.ID, from, to, loc)
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
			JoinOpen:        class.JoinOpen,
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

	recordedAt, err := u.recordedAtOf(req.RecordedAt)
	if err != nil {
		return dto.LogMoodResponse{}, err
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
		RecordedAt:       recordedAt,
	}

	if notes := strings.TrimSpace(req.Notes); notes != "" {
		moodLog.Notes = &notes
	}

	full, err := u.repo.CreateMoodLogs(ctx, student.ClassID, []entity.MoodLog{*moodLog}, u.moodDay(recordedAt))
	if err != nil {
		return dto.LogMoodResponse{}, appOrInternal(err)
	}

	if len(full) > 0 {
		return dto.LogMoodResponse{}, apperror.ErrMoodLimitReached
	}

	return dto.LogMoodResponse{LogID: moodLog.ID, RecordedAt: moodLog.RecordedAt}, nil
}

// moodDay is the local day of recordedAt, which is what the limit of mood logs per child counts in
func (u *GuruUseCase) moodDay(recordedAt time.Time) repository.MoodDay {
	from, to := clock.DayBounds(recordedAt, u.cfg.Location())

	return repository.MoodDay{From: from, To: to, Limit: constants.MoodLogsPerStudentPerDay}
}

// moodLimitError names the entries of a batch whose child has no room left, so the app can show which children to skip
func moodLimitError(entries []dto.LogMoodBatchEntry, full []uuid.UUID) *apperror.Error {
	reached := make(map[uuid.UUID]bool, len(full))
	for _, id := range full {
		reached[id] = true
	}

	details := map[string]string{}

	for i, entry := range entries {
		if reached[entry.StudentID] {
			details["entries["+strconv.Itoa(i)+"].studentId"] = "has the largest number of mood logs for that day"
		}
	}

	err := *apperror.ErrMoodLimitReached
	err.Details = details

	return &err
}

// recordedAtOf picks the time of a mood log. A log that the app queued offline names the moment the mood was seen, so a
// late replay does not take the place of a newer mood that was logged in the meantime. The time may not be in the future
// beyond the clock skew and not older than MoodRecordedAtMaxAge, and one that is inside the skew counts as now.
func (u *GuruUseCase) recordedAtOf(sent *time.Time) (time.Time, error) {
	now := u.now().UTC().Truncate(time.Microsecond)
	if sent == nil {
		return now, nil
	}

	at := sent.UTC().Truncate(time.Microsecond)

	switch {
	case at.After(now.Add(constants.MoodRecordedAtSkew)):
		return time.Time{}, apperror.Validation(map[string]string{"recordedAt": "must not be in the future"})
	case at.Before(now.Add(-constants.MoodRecordedAtMaxAge)):
		return time.Time{}, apperror.Validation(map[string]string{"recordedAt": "must not be older than 7 days"})
	case at.After(now):
		return now, nil
	default:
		return at, nil
	}
}

func (u *GuruUseCase) LogMoodBatch(ctx context.Context, userID uuid.UUID, req dto.LogMoodBatchRequest) (dto.LogMoodBatchResponse, error) {
	if len(req.Entries) < 1 || len(req.Entries) > constants.MoodBatchMax {
		return dto.LogMoodBatchResponse{}, apperror.Validation(map[string]string{
			"entries": "must hold between 1 and " + strconv.Itoa(constants.MoodBatchMax) + " items",
		})
	}

	guru, class, err := u.resolveClass(ctx, userID, &req.ClassID)
	if err != nil {
		return dto.LogMoodBatchResponse{}, err
	}

	recordedAt, err := u.recordedAtOf(req.RecordedAt)
	if err != nil {
		return dto.LogMoodBatchResponse{}, err
	}

	details := map[string]string{}
	seen := make(map[uuid.UUID]bool, len(req.Entries))
	moodLogs := make([]entity.MoodLog, 0, len(req.Entries))

	for i, entry := range req.Entries {
		path := "entries[" + strconv.Itoa(i) + "]."

		if seen[entry.StudentID] {
			details[path+"studentId"] = "appears more than once"

			continue
		}

		seen[entry.StudentID] = true

		student, err := u.repo.FindStudent(ctx, entry.StudentID)
		if err != nil {
			return dto.LogMoodBatchResponse{}, apperror.Internal(err)
		}

		// an unknown student and one from another class share a message, so the endpoint does not reveal who exists
		if student == nil || student.ClassID != class.ID {
			details[path+"studentId"] = "is not in this class"

			continue
		}

		source := entry.Source
		if source == "" {
			source = constants.SourceAICamera
		}

		confidence := float32(1)

		switch {
		case entry.ConfidenceScore != nil:
			confidence = *entry.ConfidenceScore
		case source == constants.SourceAICamera:
			details[path+"confidenceScore"] = "is required"

			continue
		}

		moodLog := entity.MoodLog{
			ID:               uuid.New(),
			StudentID:        student.ID,
			RecordedByGuruID: guru.ID,
			MoodType:         entry.MoodType,
			ConfidenceScore:  confidence,
			Source:           source,
			RecordedAt:       recordedAt,
		}

		if notes := strings.TrimSpace(entry.Notes); notes != "" {
			moodLog.Notes = &notes
		}

		moodLogs = append(moodLogs, moodLog)
	}

	if len(details) > 0 {
		return dto.LogMoodBatchResponse{}, apperror.Validation(details)
	}

	full, err := u.repo.CreateMoodLogs(ctx, class.ID, moodLogs, u.moodDay(recordedAt))
	if err != nil {
		return dto.LogMoodBatchResponse{}, appOrInternal(err)
	}

	if len(full) > 0 {
		return dto.LogMoodBatchResponse{}, moodLimitError(req.Entries, full)
	}

	logs := make([]dto.LogMoodBatchItem, len(moodLogs))
	for i, moodLog := range moodLogs {
		logs[i] = dto.LogMoodBatchItem{LogID: moodLog.ID, StudentID: moodLog.StudentID, RecordedAt: moodLog.RecordedAt}
	}

	return dto.LogMoodBatchResponse{Logs: logs}, nil
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

	records, err := u.repo.ListMoodRecords(ctx, class.ID, queryFrom, queryTo, loc)
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

// resolveWritableClass is resolveClass for a change, which an archived class refuses because it is read only
func (u *GuruUseCase) resolveWritableClass(
	ctx context.Context, userID uuid.UUID, classID *uuid.UUID,
) (*repository.GuruRow, *entity.Class, error) {
	guru, class, err := u.resolveClass(ctx, userID, classID)
	if err != nil {
		return nil, nil, err
	}

	if class.ArchivedAt != nil {
		return nil, nil, apperror.ErrClassArchived
	}

	return guru, class, nil
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
	guru, class, err := u.resolveWritableClass(ctx, userID, &classID)
	if err != nil {
		return dto.JoinCodeResponse{}, err
	}

	code, err := u.repo.RotateJoinCode(ctx, class.ID, guru.ID)
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return dto.JoinCodeResponse{}, err
		}

		return dto.JoinCodeResponse{}, apperror.Internal(err)
	}

	return dto.JoinCodeResponse{JoinCode: code}, nil
}

func (u *GuruUseCase) SetJoining(
	ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.SetJoiningRequest,
) (dto.JoiningResponse, error) {
	_, class, err := u.resolveWritableClass(ctx, userID, &classID)
	if err != nil {
		return dto.JoiningResponse{}, err
	}

	if req.JoinOpen == nil {
		return dto.JoiningResponse{}, apperror.Validation(map[string]string{"joinOpen": "is required"})
	}

	err = u.repo.SetJoining(ctx, class.ID, *req.JoinOpen)
	if err != nil {
		return dto.JoiningResponse{}, apperror.Internal(err)
	}

	return dto.JoiningResponse{JoinOpen: *req.JoinOpen}, nil
}

func (u *GuruUseCase) RemoveStudent(ctx context.Context, userID uuid.UUID, classID uuid.UUID, studentID uuid.UUID) error {
	_, class, err := u.resolveWritableClass(ctx, userID, &classID)
	if err != nil {
		return err
	}

	removed, err := u.repo.RemoveStudent(ctx, class.ID, studentID)
	if err != nil {
		return apperror.Internal(err)
	}

	if !removed {
		return apperror.ErrStudentNotFound
	}

	// the cache is optional, entries also expire by themselves
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+class.ID.String())

	return nil
}

func (u *GuruUseCase) Teachers(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.TeacherList, error) {
	_, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return dto.TeacherList{}, err
	}

	rows, err := u.repo.ListTeachers(ctx, class.ID)
	if err != nil {
		return dto.TeacherList{}, apperror.Internal(err)
	}

	res := dto.TeacherList{Teachers: make([]dto.Teacher, 0, len(rows))}
	for i := range rows {
		res.Teachers = append(res.Teachers, teacherResponse(&rows[i], userID))
	}

	return res, nil
}

func (u *GuruUseCase) AddTeacher(
	ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.AddTeacherRequest,
) (dto.Teacher, error) {
	guru, class, err := u.resolveWritableClass(ctx, userID, &classID)
	if err != nil {
		return dto.Teacher{}, err
	}

	added, err := u.repo.AddTeacher(ctx, class.ID, guru.ID, strings.ToLower(strings.TrimSpace(req.Email)))
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return dto.Teacher{}, err
		}

		return dto.Teacher{}, apperror.Internal(err)
	}

	if added == nil {
		return dto.Teacher{}, apperror.ErrTeacherNotFound
	}

	return teacherResponse(added, userID), nil
}

func (u *GuruUseCase) RemoveTeacher(ctx context.Context, userID uuid.UUID, classID uuid.UUID, teacherID uuid.UUID) error {
	guru, class, err := u.resolveWritableClass(ctx, userID, &classID)
	if err != nil {
		return err
	}

	return teamError(u.repo.RemoveTeacher(ctx, class.ID, guru.ID, teacherID))
}

func (u *GuruUseCase) TransferOwnership(
	ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.TransferOwnershipRequest,
) (dto.Teacher, error) {
	guru, class, err := u.resolveWritableClass(ctx, userID, &classID)
	if err != nil {
		return dto.Teacher{}, err
	}

	owner, err := u.repo.TransferOwnership(ctx, class.ID, guru.ID, req.TeacherID)
	if err != nil {
		return dto.Teacher{}, teamError(err)
	}

	// the teacher left or was deleted between the check and the lock
	if owner == nil {
		return dto.Teacher{}, apperror.ErrTeacherNotInClass
	}

	return teacherResponse(owner, userID), nil
}

// teamError keeps a refusal of the repository as it is and turns any other failure into an internal error
func teamError(err error) error {
	if err == nil {
		return nil
	}

	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return err
	}

	return apperror.Internal(err)
}

func teacherResponse(row *repository.TeacherRow, viewer uuid.UUID) dto.Teacher {
	return dto.Teacher{
		ID: row.ID, UserID: row.UserID, IsSelf: row.UserID == viewer, FullName: row.FullName, NIP: row.NIP,
		AvatarURL: row.AvatarURL, IsOwner: row.IsOwner, JoinedAt: row.JoinedAt,
	}
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
		JoinOpen:     true,
	}

	err = u.repo.CreateClass(ctx, guru.ID, class)
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return dto.ClassSummary{}, err
		}

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

	if req.FullName == nil && req.PhoneNumber == nil && req.Address == nil {
		return update, apperror.Validation(map[string]string{"body": "provide fullName, phoneNumber or address"})
	}

	if req.FullName != nil {
		name := strings.TrimSpace(*req.FullName)
		if name == "" {
			return update, apperror.Validation(map[string]string{"fullName": "is required"})
		}

		update.SetName, update.FullName = true, name
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
		JoinOpen:      row.JoinOpen,
		TotalStudents: row.TotalStudents,
		CreatedAt:     row.CreatedAt.UTC(),
		ArchivedAt:    archivedAt(row.ArchivedAt),
	}
}

func archivedAt(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}

	utc := at.UTC()

	return &utc
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

func (u *GuruUseCase) MoveRequests(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassMoveRequestList, error) {
	_, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return dto.ClassMoveRequestList{}, err
	}

	rows, err := u.repo.ListMoveRequests(ctx, class.ID)
	if err != nil {
		return dto.ClassMoveRequestList{}, apperror.Internal(err)
	}

	res := dto.ClassMoveRequestList{Requests: make([]dto.ClassMoveRequest, 0, len(rows))}
	for i := range rows {
		res.Requests = append(res.Requests, dto.ClassMoveRequest{
			ID: rows[i].ID, StudentID: rows[i].StudentID, StudentName: rows[i].StudentName, RequestedAt: rows[i].RequestedAt.UTC(),
		})
	}

	return res, nil
}

func (u *GuruUseCase) AcceptMove(ctx context.Context, userID uuid.UUID, classID uuid.UUID, requestID uuid.UUID) error {
	guru, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return err
	}

	left, err := u.repo.AcceptMove(ctx, class.ID, requestID, guru.ID)
	if err != nil {
		return appOrInternal(err)
	}

	// both classes changed their ranking, and the cache is optional because entries also expire by themselves
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+class.ID.String())
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+left.String())

	return nil
}

func (u *GuruUseCase) RejectMove(ctx context.Context, userID uuid.UUID, classID uuid.UUID, requestID uuid.UUID) error {
	guru, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return err
	}

	err = u.repo.RejectMove(ctx, class.ID, requestID, guru.ID)
	if err != nil {
		return appOrInternal(err)
	}

	return nil
}

// appOrInternal keeps an error that already has an API answer and hides any other one behind a 500
func appOrInternal(err error) error {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return err
	}

	return apperror.Internal(err)
}

func (u *GuruUseCase) ArchiveClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassSummary, error) {
	guru, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return dto.ClassSummary{}, err
	}

	err = u.repo.ArchiveClass(ctx, class.ID, guru.ID)
	if err != nil {
		return dto.ClassSummary{}, appOrInternal(err)
	}

	// the points of the class are frozen from now on, and the leaderboard is read from the cache
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+class.ID.String())

	return u.classSummaryOf(ctx, guru.ID, class.ID)
}

func (u *GuruUseCase) RestoreClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ClassSummary, error) {
	guru, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return dto.ClassSummary{}, err
	}

	_, err = u.repo.RestoreClass(ctx, class.ID, guru.ID)
	if err != nil {
		return dto.ClassSummary{}, appOrInternal(err)
	}

	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+class.ID.String())

	return u.classSummaryOf(ctx, guru.ID, class.ID)
}

// classSummaryOf reads the class again after a change, so the answer holds the new join code and the archive time
func (u *GuruUseCase) classSummaryOf(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (dto.ClassSummary, error) {
	row, err := u.repo.FindClassRow(ctx, guruID, classID)
	if err != nil {
		return dto.ClassSummary{}, apperror.Internal(err)
	}

	if row == nil {
		return dto.ClassSummary{}, apperror.ErrClassNotFound
	}

	return classSummary(row), nil
}

func (u *GuruUseCase) StudentHistory(
	ctx context.Context, userID uuid.UUID, classID uuid.UUID, studentID uuid.UUID,
) (dto.StudentHistory, error) {
	_, class, err := u.resolveClass(ctx, userID, &classID)
	if err != nil {
		return dto.StudentHistory{}, err
	}

	rows, err := u.repo.StudentHistory(ctx, class.ID, studentID)
	if err != nil {
		return dto.StudentHistory{}, appOrInternal(err)
	}

	res := dto.StudentHistory{PreviousClasses: make([]dto.HistoryClass, 0, len(rows))}

	for i := range rows {
		item := dto.HistoryClass{
			PreviousClass: dto.PreviousClass{
				ClassID: rows[i].ClassID, ClassName: rows[i].ClassName, GradeLevel: rows[i].GradeLevel, SchoolName: rows[i].SchoolName,
				AcademicYear: rows[i].AcademicYear, StartedAt: rows[i].ActivatedAt.UTC(), EndedAt: rows[i].EndedAt.UTC(),
				TotalPoints: rows[i].FinalPoints, SeasonPoints: rows[i].FinalSeason, ClassRank: rows[i].FinalRank, TotalScans: rows[i].FinalScans,
			},
			Breakdown:      make([]dto.TrashTypeStat, 0, len(rows[i].Breakdown)),
			Moods:          make([]dto.HistoryMood, 0, len(rows[i].Moods)),
			MoodsTruncated: rows[i].MoodsTruncated,
		}

		for _, totals := range rows[i].Breakdown {
			item.Breakdown = append(item.Breakdown, dto.TrashTypeStat{TrashType: totals.TrashType, Scans: totals.Scans, Points: totals.Points})
		}

		for _, mood := range rows[i].Moods {
			item.Moods = append(item.Moods, dto.HistoryMood{
				MoodType: mood.MoodType, Label: constants.MoodLabels[mood.MoodType], ConfidenceScore: mood.ConfidenceScore,
				Notes: mood.Notes, RecordedAt: mood.RecordedAt.UTC(),
			})
		}

		res.PreviousClasses = append(res.PreviousClasses, item)
	}

	return res, nil
}
