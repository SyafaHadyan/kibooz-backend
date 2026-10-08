// Package usecase holds the parent portal business rules
package usecase

import (
	"context"
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
)

// trashTypes fixes the order of the categories in the statistics
var trashTypes = []constants.TrashType{constants.TrashOrganik, constants.TrashAnorganik, constants.TrashB3}

type WaliUseCaseItf interface {
	Dashboard(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID) (dto.WaliDashboardResponse, error)
	ApplyGuidance(ctx context.Context, userID uuid.UUID, req dto.ApplyGuidanceRequest) error
	// Child shows the registered details of one child of the caller
	Child(ctx context.Context, userID uuid.UUID, studentID uuid.UUID) (dto.ChildProfile, error)
	// TrashStats shows what a child collected per trash category, for the oldest child when studentID is nil
	TrashStats(ctx context.Context, userID uuid.UUID, studentID *uuid.UUID) (dto.TrashStatsResponse, error)
}

type WaliUseCase struct {
	repo repository.WaliDBItf
	cfg  *env.Env
	now  func() time.Time
}

func NewWaliUseCase(repo repository.WaliDBItf, cfg *env.Env) WaliUseCaseItf {
	return &WaliUseCase{repo: repo, cfg: cfg, now: time.Now}
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
		ClassRank:     student.RankPosition,
		Guardian: dto.Guardian{
			FullName:       guardian.FullName,
			Email:          guardian.Email,
			PhoneNumber:    guardian.PhoneNumber,
			WhatsappNumber: guardian.WhatsappNumber,
			Address:        guardian.Address,
		},
	}, nil
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
