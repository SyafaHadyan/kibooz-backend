// Package usecase holds the parent portal business rules
package usecase

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/clock"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/enrollment"
	"github.com/SyafaHadyan/kibooz-backend/internal/guidance"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
)

// trashTypes fixes the order of the categories in the statistics
var trashTypes = []constants.TrashType{constants.TrashOrganik, constants.TrashAnorganik, constants.TrashB3}

type WaliUseCaseItf interface {
	Dashboard(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID) (dto.WaliDashboardResponse, error)
	ApplyGuidance(ctx context.Context, userID uuid.UUID, req dto.ApplyGuidanceRequest) error
	// Children lists the children of the caller with their classes
	Children(ctx context.Context, userID uuid.UUID) (dto.ChildList, error)
	// AddChild registers one more child of the caller in the class that owns the class code
	AddChild(ctx context.Context, userID uuid.UUID, req dto.AddChildRequest) (dto.ChildSummary, error)
	// UpdateProfile changes the name, phone number, WhatsApp number or address of the caller and returns the new details
	UpdateProfile(ctx context.Context, userID uuid.UUID, req dto.UpdateWaliProfileRequest) (dto.Guardian, error)
	// DeleteChild hides a child of the caller the way a deleted account does, the caller may delete the last child
	DeleteChild(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) error
	// UpdateChild changes the name or the NISN of a child of the caller
	UpdateChild(ctx context.Context, userID uuid.UUID, studentID uuid.UUID, req dto.UpdateChildRequest) (dto.ChildSummary, error)
	// Child shows the registered details of one child of the caller
	Child(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.ChildProfile, error)
	// TrashStats shows what a child collected per trash category, for the oldest child when studentID is nil
	// With a period only the scans of that day, week or month are counted in the breakdown and the totals
	TrashStats(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID, period constants.Period) (dto.TrashStatsResponse, error)
	// RequestMove asks for a child of the caller to move to the class that owns the class code, a teacher of that class decides
	RequestMove(ctx context.Context, userID uuid.UUID, studentID uuid.UUID, req dto.MoveRequestInput) (dto.MoveRequest, error)
	// CancelMove withdraws a request of the caller that no teacher has decided yet
	CancelMove(ctx context.Context, userID uuid.UUID, studentID uuid.UUID, requestID uuid.UUID) error
	// MoveRequests lists the requests of a child of the caller that wait or were rejected
	MoveRequests(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.MoveRequestList, error)
	// PreviousClasses lists the classes that a child of the caller left, with what the child had in each
	PreviousClasses(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.PreviousClassList, error)
}

// MaxChildren is the largest number of children one parent account can hold
const MaxChildren = 10

type WaliUseCase struct {
	repo  repository.WaliDBItf
	cache redis.CacheItf
	cfg   *env.Env
	now   func() time.Time
}

func NewWaliUseCase(repo repository.WaliDBItf, cache redis.CacheItf, cfg *env.Env) WaliUseCaseItf {
	return &WaliUseCase{repo: repo, cache: cache, cfg: cfg, now: time.Now}
}

func (u *WaliUseCase) Dashboard(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID) (dto.WaliDashboardResponse, error) {
	student, err := u.resolveStudent(ctx, userID, studentID)
	if err != nil {
		return dto.WaliDashboardResponse{}, err
	}

	from, to := clock.DayBounds(u.now(), u.cfg.Location())

	moodLog, err := u.repo.LatestMood(ctx, student.ID, from, to)
	if err != nil {
		return dto.WaliDashboardResponse{}, apperror.Internal(err)
	}

	counts, err := u.repo.CountTrashByType(ctx, student.ID)
	if err != nil {
		return dto.WaliDashboardResponse{}, apperror.Internal(err)
	}

	res := dto.WaliDashboardResponse{
		Student: dto.WaliStudent{
			ID:        student.ID,
			FullName:  student.FullName,
			ClassID:   student.ClassID,
			ClassName: fmt.Sprintf("%s • %s", student.GradeLevel, student.SchoolName),
			AvatarURL: student.AvatarURL,
			NISN:      student.NISN,
		},
		PointsSummary: dto.PointsSummary{
			TotalPoints:    student.CurrentPoints,
			SeasonPoints:   student.SeasonPoints,
			ClassRank:      student.RankPosition,
			OrganicCount:   counts[constants.TrashOrganik],
			AnorganicCount: counts[constants.TrashAnorganik],
		},
	}

	if moodLog != nil {
		res.TodayMood = &dto.TodayMood{
			MoodType:     moodLog.MoodType,
			Label:        constants.MoodLabels[moodLog.MoodType],
			Confidence:   moodLog.ConfidenceScore,
			RecordedAt:   moodLog.RecordedAt.UTC(),
			TeacherNotes: moodLog.Notes,
		}

		item, ok := guidance.ForMood(moodLog.MoodType)
		if ok {
			res.RecommendedGuidance = &dto.RecommendedGuidance{
				ID:        item.ID,
				Title:     item.Title,
				Category:  item.Category,
				BannerURL: item.BannerURL(u.cfg.S3PublicURL),
			}
		}
	}

	return res, nil
}

func (u *WaliUseCase) ApplyGuidance(ctx context.Context, userID uuid.UUID, req dto.ApplyGuidanceRequest) error {
	if !guidance.Exists(req.GuidanceID) {
		return apperror.ErrGuidanceNotFound
	}

	student, err := u.resolveStudent(ctx, userID, &req.StudentID)
	if err != nil {
		return err
	}

	notes := strings.TrimSpace(req.ParentNotes)

	application := &entity.GuidanceApplication{
		ID:         uuid.New(),
		StudentID:  student.ID,
		WaliID:     student.WaliID,
		GuidanceID: req.GuidanceID,
		AppliedAt:  u.now().UTC(),
	}

	if notes != "" {
		application.ParentNotes = &notes
	}

	err = u.repo.CreateGuidanceApplication(ctx, application)
	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (u *WaliUseCase) Child(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.ChildProfile, error) {
	student, err := u.resolveStudent(ctx, userID, &studentID)
	if err != nil {
		return dto.ChildProfile{}, err
	}

	guardian, err := u.repo.FindGuardian(ctx, userID)
	if err != nil {
		return dto.ChildProfile{}, apperror.Internal(err)
	}

	if guardian == nil {
		return dto.ChildProfile{}, apperror.ErrProfileNotFound
	}

	return dto.ChildProfile{
		ID:            student.ID,
		FullName:      student.FullName,
		NISN:          student.NISN,
		AvatarURL:     student.AvatarURL,
		ClassID:       student.ClassID,
		ClassName:     student.ClassName,
		GradeLevel:    student.GradeLevel,
		SchoolName:    student.SchoolName,
		AcademicYear:  student.AcademicYear,
		CurrentPoints: student.CurrentPoints,
		SeasonPoints:  student.SeasonPoints,
		ClassRank:     student.RankPosition,
		Guardian:      guardianOf(guardian),
	}, nil
}

func guardianOf(row *repository.GuardianRow) dto.Guardian {
	return dto.Guardian{
		FullName:       row.FullName,
		Email:          row.Email,
		PhoneNumber:    row.PhoneNumber,
		WhatsappNumber: row.WhatsappNumber,
		Address:        row.Address,
	}
}

func (u *WaliUseCase) UpdateProfile(ctx context.Context, userID uuid.UUID, req dto.UpdateWaliProfileRequest) (dto.Guardian, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return dto.Guardian{}, apperror.Internal(err)
	}

	if wali == nil {
		return dto.Guardian{}, apperror.ErrProfileNotFound
	}

	update, err := profileUpdate(req)
	if err != nil {
		return dto.Guardian{}, err
	}

	err = u.repo.UpdateProfile(ctx, userID, wali.ID, update)
	if err != nil {
		return dto.Guardian{}, apperror.Internal(err)
	}

	guardian, err := u.repo.FindGuardian(ctx, userID)
	if err != nil {
		return dto.Guardian{}, apperror.Internal(err)
	}

	if guardian == nil {
		return dto.Guardian{}, apperror.ErrProfileNotFound
	}

	return guardianOf(guardian), nil
}

// profileUpdate checks the changed fields. An empty text clears a phone number, a WhatsApp number or the address, and a
// missing field is left alone.
func profileUpdate(req dto.UpdateWaliProfileRequest) (repository.ProfileUpdate, error) {
	var update repository.ProfileUpdate

	if req.FullName == nil && req.PhoneNumber == nil && req.WhatsappNumber == nil && req.Address == nil {
		return update, apperror.Validation(map[string]string{"body": "provide fullName, phoneNumber, whatsappNumber or address"})
	}

	invalid := map[string]string{}

	if req.FullName != nil {
		name := strings.TrimSpace(*req.FullName)
		if name == "" {
			invalid["fullName"] = "is required"
		}

		update.SetName, update.FullName = true, name
	}

	var ok bool

	update.SetPhone, update.Phone, ok = phoneOf(req.PhoneNumber)
	if !ok {
		invalid["phoneNumber"] = "must be a valid phone number"
	}

	update.SetWhatsapp, update.Whatsapp, ok = phoneOf(req.WhatsappNumber)
	if !ok {
		invalid["whatsappNumber"] = "must be a valid phone number"
	}

	if req.Address != nil {
		update.SetAddress, update.Address = true, optionalText(*req.Address)
	}

	if len(invalid) > 0 {
		return update, apperror.Validation(invalid)
	}

	return update, nil
}

// phoneOf reads a phone number that may be missing, which is not set, or empty, which clears it. The last result is false
// for a text that is not a phone number.
func phoneOf(raw *string) (set bool, value *string, valid bool) {
	if raw == nil {
		return false, nil, true
	}

	phone := strings.TrimSpace(*raw)
	if phone != "" && !validation.PhonePattern.MatchString(phone) {
		return true, nil, false
	}

	return true, optionalText(phone), true
}

func optionalText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return &value
}

func (u *WaliUseCase) TrashStats(
	ctx context.Context, userID uuid.UUID, studentID *uuid.UUID, period constants.Period,
) (dto.TrashStatsResponse, error) {
	student, err := u.resolveStudent(ctx, userID, studentID)
	if err != nil {
		return dto.TrashStatsResponse{}, err
	}

	var totals map[constants.TrashType]repository.TrashTotals

	if period == "" {
		totals, err = u.repo.SumTrashByType(ctx, student.ID)
	} else {
		windowFrom, windowTo := clock.PeriodBounds(period, u.now(), u.cfg.Location())
		totals, err = u.repo.SumTrashByTypeBetween(ctx, student.ID, windowFrom, windowTo)
	}

	if err != nil {
		return dto.TrashStatsResponse{}, apperror.Internal(err)
	}

	from, to := clock.DayBounds(u.now(), u.cfg.Location())

	today, err := u.repo.CountScans(ctx, student.ID, from, to)
	if err != nil {
		return dto.TrashStatsResponse{}, apperror.Internal(err)
	}

	res := dto.TrashStatsResponse{
		Student:             dto.TrashStatsStudent{ID: student.ID, FullName: student.FullName},
		TotalPoints:         student.CurrentPoints,
		SeasonPoints:        student.SeasonPoints,
		ClassRank:           student.RankPosition,
		Breakdown:           make([]dto.TrashTypeStat, 0, len(trashTypes)),
		TodayScans:          today,
		DailyLimit:          u.cfg.TrashDailyLimit,
		RemainingDailyScans: max(u.cfg.TrashDailyLimit-today, 0),
	}

	periodPoints := 0

	for _, trashType := range trashTypes {
		item := totals[trashType]

		res.Breakdown = append(res.Breakdown, dto.TrashTypeStat{TrashType: trashType, Scans: item.Scans, Points: item.Points})
		res.TotalScans += item.Scans
		periodPoints += item.Points
	}

	if period != "" {
		res.Period = string(period)
		res.PeriodPoints = &periodPoints
	}

	return res, nil
}

// resolveStudent only ever returns a child that belongs to the caller
func (u *WaliUseCase) Children(ctx context.Context, userID uuid.UUID) (dto.ChildList, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return dto.ChildList{}, apperror.Internal(err)
	}

	if wali == nil {
		return dto.ChildList{}, apperror.ErrProfileNotFound
	}

	rows, err := u.repo.ListChildren(ctx, wali.ID)
	if err != nil {
		return dto.ChildList{}, apperror.Internal(err)
	}

	res := dto.ChildList{Children: make([]dto.ChildSummary, 0, len(rows))}
	for i := range rows {
		res.Children = append(res.Children, childSummary(&rows[i]))
	}

	return res, nil
}

func (u *WaliUseCase) AddChild(ctx context.Context, userID uuid.UUID, req dto.AddChildRequest) (dto.ChildSummary, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return dto.ChildSummary{}, apperror.Internal(err)
	}

	if wali == nil {
		return dto.ChildSummary{}, apperror.ErrProfileNotFound
	}

	student := &entity.Student{
		ID:       uuid.New(),
		NISN:     strings.TrimSpace(req.Student.NISN),
		FullName: strings.TrimSpace(req.Student.FullName),
	}

	classID, err := u.repo.AddChild(ctx, wali.ID, strings.ToUpper(strings.TrimSpace(req.ClassCode)), student, MaxChildren)
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return dto.ChildSummary{}, err
		}

		return dto.ChildSummary{}, apperror.Internal(err)
	}

	// a new child changes the ranking so the cached leaderboard is stale
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	created, err := u.repo.FindStudent(ctx, wali.ID, &student.ID)
	if err != nil {
		return dto.ChildSummary{}, apperror.Internal(err)
	}

	if created == nil {
		return dto.ChildSummary{}, apperror.ErrStudentNotFound
	}

	return childSummary(created), nil
}

func (u *WaliUseCase) DeleteChild(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) error {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if wali == nil {
		return apperror.ErrProfileNotFound
	}

	classID, removed, err := u.repo.RemoveStudent(ctx, wali.ID, studentID)
	if err != nil {
		return apperror.Internal(err)
	}

	if !removed {
		return apperror.ErrStudentNotFound
	}

	// the child leaves the leaderboard, so the cached one is stale
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	return nil
}

func (u *WaliUseCase) UpdateChild(
	ctx context.Context, userID uuid.UUID, studentID uuid.UUID, req dto.UpdateChildRequest,
) (dto.ChildSummary, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return dto.ChildSummary{}, apperror.Internal(err)
	}

	if wali == nil {
		return dto.ChildSummary{}, apperror.ErrProfileNotFound
	}

	update, err := childUpdate(req)
	if err != nil {
		return dto.ChildSummary{}, err
	}

	classID, found, err := u.repo.UpdateStudent(ctx, wali.ID, studentID, update)
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return dto.ChildSummary{}, err
		}

		return dto.ChildSummary{}, apperror.Internal(err)
	}

	if !found {
		return dto.ChildSummary{}, apperror.ErrStudentNotFound
	}

	// a new name changes the leaderboard and its order, so the cached one is stale
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	child, err := u.repo.FindStudent(ctx, wali.ID, &studentID)
	if err != nil {
		return dto.ChildSummary{}, apperror.Internal(err)
	}

	if child == nil {
		return dto.ChildSummary{}, apperror.ErrStudentNotFound
	}

	return childSummary(child), nil
}

// childUpdate checks the changed fields with the same rules as the registration of a child
func childUpdate(req dto.UpdateChildRequest) (repository.StudentUpdate, error) {
	var update repository.StudentUpdate

	if req.FullName == nil && req.NISN == nil {
		return update, apperror.Validation(map[string]string{"body": "provide fullName or nisn"})
	}

	invalid := map[string]string{}

	if req.FullName != nil {
		name := strings.TrimSpace(*req.FullName)
		if utf8.RuneCountInString(name) < 2 {
			invalid["fullName"] = "must have at least 2 characters"
		}

		update.SetName, update.FullName = true, name
	}

	if req.NISN != nil {
		nisn := strings.TrimSpace(*req.NISN)
		if !nisnPattern.MatchString(nisn) {
			invalid["nisn"] = "must be a number of 5 to 30 digits"
		}

		update.SetNISN, update.NISN = true, nisn
	}

	if len(invalid) > 0 {
		return update, apperror.Validation(invalid)
	}

	return update, nil
}

// nisnPattern is what registration accepts for a NISN, a number of 5 to 30 digits
var nisnPattern = regexp.MustCompile(`^[0-9]{5,30}$`)

func childSummary(row *repository.StudentRow) dto.ChildSummary {
	return dto.ChildSummary{
		ID:        row.ID,
		FullName:  row.FullName,
		NISN:      row.NISN,
		AvatarURL: row.AvatarURL,
		ClassID:   row.ClassID,
		ClassName: row.ClassName,
	}
}

func (u *WaliUseCase) resolveStudent(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID) (*repository.StudentRow, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if wali == nil {
		return nil, apperror.ErrProfileNotFound
	}

	student, err := u.repo.FindStudent(ctx, wali.ID, studentID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if student == nil && studentID == nil {
		// a parent can delete every child, and the dashboard then has nothing to show
		return nil, apperror.ErrNoChild
	}

	if student == nil {
		return nil, apperror.ErrStudentNotFound
	}

	return student, nil
}

func (u *WaliUseCase) RequestMove(
	ctx context.Context, userID uuid.UUID, studentID uuid.UUID, req dto.MoveRequestInput,
) (dto.MoveRequest, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return dto.MoveRequest{}, apperror.Internal(err)
	}

	if wali == nil {
		return dto.MoveRequest{}, apperror.ErrProfileNotFound
	}

	row, err := u.repo.RequestMove(ctx, wali.ID, studentID, strings.ToUpper(strings.TrimSpace(req.ClassCode)))
	if err != nil {
		return dto.MoveRequest{}, appOrInternal(err)
	}

	return moveRequest(&row), nil
}

func (u *WaliUseCase) CancelMove(ctx context.Context, userID uuid.UUID, studentID uuid.UUID, requestID uuid.UUID) error {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if wali == nil {
		return apperror.ErrProfileNotFound
	}

	err = u.repo.CancelMove(ctx, wali.ID, studentID, requestID)
	if err != nil {
		return appOrInternal(err)
	}

	return nil
}

func (u *WaliUseCase) MoveRequests(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.MoveRequestList, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return dto.MoveRequestList{}, apperror.Internal(err)
	}

	if wali == nil {
		return dto.MoveRequestList{}, apperror.ErrProfileNotFound
	}

	rows, err := u.repo.ListMoves(ctx, wali.ID, studentID)
	if err != nil {
		return dto.MoveRequestList{}, appOrInternal(err)
	}

	res := dto.MoveRequestList{Requests: make([]dto.MoveRequest, 0, len(rows))}
	for i := range rows {
		res.Requests = append(res.Requests, moveRequest(&rows[i]))
	}

	return res, nil
}

// appOrInternal keeps an error that already has an API answer and hides any other one behind a 500
func appOrInternal(err error) error {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return err
	}

	return apperror.Internal(err)
}

func moveRequest(row *enrollment.ParentRow) dto.MoveRequest {
	res := dto.MoveRequest{
		ID: row.ID, StudentID: row.StudentID, ClassID: row.ClassID, ClassName: row.ClassName,
		Status: string(row.Status), RequestedAt: row.RequestedAt.UTC(),
	}

	if row.DecidedAt != nil {
		decided := row.DecidedAt.UTC()
		res.DecidedAt = &decided
	}

	return res
}

func (u *WaliUseCase) PreviousClasses(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.PreviousClassList, error) {
	wali, err := u.repo.FindWaliByUserID(ctx, userID)
	if err != nil {
		return dto.PreviousClassList{}, apperror.Internal(err)
	}

	if wali == nil {
		return dto.PreviousClassList{}, apperror.ErrProfileNotFound
	}

	rows, err := u.repo.PreviousClasses(ctx, wali.ID, studentID)
	if err != nil {
		return dto.PreviousClassList{}, appOrInternal(err)
	}

	return previousClasses(rows), nil
}

func previousClasses(rows []enrollment.PreviousRow) dto.PreviousClassList {
	res := dto.PreviousClassList{Classes: make([]dto.PreviousClass, 0, len(rows))}

	for i := range rows {
		res.Classes = append(res.Classes, dto.PreviousClass{
			ClassID: rows[i].ClassID, ClassName: rows[i].ClassName, GradeLevel: rows[i].GradeLevel, SchoolName: rows[i].SchoolName,
			AcademicYear: rows[i].AcademicYear, StartedAt: rows[i].ActivatedAt.UTC(), EndedAt: rows[i].EndedAt.UTC(),
			TotalPoints: rows[i].FinalPoints, SeasonPoints: rows[i].FinalSeason, ClassRank: rows[i].FinalRank, TotalScans: rows[i].FinalScans,
		})
	}

	return res
}
