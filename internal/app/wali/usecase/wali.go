// Package usecase holds the parent portal business rules
package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/clock"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
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
	// Child shows the registered details of one child of the caller
	Child(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.ChildProfile, error)
	// TrashStats shows what a child collected per trash category, for the oldest child when studentID is nil
	TrashStats(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID) (dto.TrashStatsResponse, error)
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

func (u *WaliUseCase) TrashStats(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID) (dto.TrashStatsResponse, error) {
	student, err := u.resolveStudent(ctx, userID, studentID)
	if err != nil {
		return dto.TrashStatsResponse{}, err
	}

	totals, err := u.repo.SumTrashByType(ctx, student.ID)
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

	for _, trashType := range trashTypes {
		item := totals[trashType]

		res.Breakdown = append(res.Breakdown, dto.TrashTypeStat{TrashType: trashType, Scans: item.Scans, Points: item.Points})
		res.TotalScans += item.Scans
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

	if student == nil {
		return nil, apperror.ErrStudentNotFound
	}

	return student, nil
}
