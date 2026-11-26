package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/app/classroom/repository"
	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/domain/entity"
	"github.com/kiboozapp/kibooz-backend/internal/pagination"
)

type moduleRepo struct {
	repository.ClassroomDBItf

	teacher  *uuid.UUID
	parent   bool
	archived bool

	modules  []entity.LearningModule
	total    int
	created  *entity.LearningModule
	updated  *entity.LearningModule
	deleted  bool
	listErr  error
	writeErr error
}

func (r *moduleRepo) TeacherOfClass(context.Context, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	return r.teacher, nil
}

func (r *moduleRepo) ParentInClass(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return r.parent, nil
}

func (r *moduleRepo) ClassArchived(context.Context, uuid.UUID) (bool, error) {
	return r.archived, nil
}

func (r *moduleRepo) ListModules(context.Context, uuid.UUID, int, int) ([]entity.LearningModule, int, error) {
	return r.modules, r.total, r.listErr
}

func (r *moduleRepo) CreateModule(_ context.Context, module *entity.LearningModule) error {
	if r.writeErr != nil {
		return r.writeErr
	}

	r.created = module

	return nil
}

func (r *moduleRepo) UpdateModule(
	_ context.Context, _ uuid.UUID, _ uuid.UUID, change func(module *entity.LearningModule) error,
) (*entity.LearningModule, error) {
	if r.writeErr != nil {
		return nil, r.writeErr
	}

	if r.updated == nil {
		return nil, nil
	}

	copyRow := *r.updated
	if err := change(&copyRow); err != nil {
		return nil, err
	}

	r.updated = &copyRow

	return &copyRow, nil
}

func (r *moduleRepo) DeleteModule(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	if r.writeErr != nil {
		return false, r.writeErr
	}

	return r.deleted, nil
}

func TestListModules(t *testing.T) {
	ctx := context.Background()
	classID, guruID := uuid.New(), uuid.New()
	desc := "Mengenal warna emosi"
	sched := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)

	repo := &moduleRepo{
		teacher: &guruID,
		parent:  true,
		total:   1,
		modules: []entity.LearningModule{{
			ID: uuid.New(), ClassID: classID, AddedByGuruID: guruID, MeetingNumber: 1,
			Title: "Pertemuan 1", Description: &desc, Status: constants.ModuleOngoing,
			ScheduledAt: &sched, CreatedAt: sched, UpdatedAt: sched,
		}},
	}
	u := &ClassroomUseCase{repo: repo}
	page := pagination.Params{Page: 1, Limit: 20}

	res, err := u.ListModules(ctx, uuid.New(), constants.RoleWali, classID, page)
	require.NoError(t, err)
	require.Equal(t, 1, res.Total)
	require.Len(t, res.Modules, 1)
	require.Equal(t, "Pertemuan 1", res.Modules[0].Title)
	require.Equal(t, constants.ModuleOngoing, res.Modules[0].Status)

	repo.listErr = errors.New("db down")
	_, err = u.ListModules(ctx, uuid.New(), constants.RoleGuru, classID, page)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)

	repo.parent = false
	_, err = u.ListModules(ctx, uuid.New(), constants.RoleWali, classID, page)
	require.ErrorIs(t, err, apperror.ErrForbidden)
}

func TestCreateModule(t *testing.T) {
	ctx := context.Background()
	classID, guruID := uuid.New(), uuid.New()
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	sched := time.Date(2026, 11, 28, 7, 30, 0, 0, time.UTC)

	repo := &moduleRepo{teacher: &guruID}
	u := &ClassroomUseCase{repo: repo, now: func() time.Time { return now }}

	res, err := u.CreateModule(ctx, uuid.New(), classID, dto.CreateModuleRequest{
		MeetingNumber: 1,
		Title:         "  Mengenal Emosi  ",
		Description:   "   ",
		ScheduledAt:   &sched,
	})
	require.NoError(t, err)
	require.Equal(t, "Mengenal Emosi", res.Title)
	require.Nil(t, res.Description)
	require.Equal(t, constants.ModuleUpcoming, res.Status)
	require.Equal(t, 1, res.MeetingNumber)
	require.NotNil(t, res.ScheduledAt)
	require.Equal(t, sched, *res.ScheduledAt)
	require.Equal(t, now, repo.created.CreatedAt)

	_, err = u.CreateModule(ctx, uuid.New(), classID, dto.CreateModuleRequest{MeetingNumber: 1, Title: "   "})
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "VALIDATION_ERROR", appErr.Code)

	repo.writeErr = errors.New("insert failed")
	_, err = u.CreateModule(ctx, uuid.New(), classID, dto.CreateModuleRequest{MeetingNumber: 1, Title: "Valid"})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)
}

func TestUpdateAndDeleteModule(t *testing.T) {
	ctx := context.Background()
	classID, guruID, moduleID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	desc := "Lama"
	repo := &moduleRepo{
		teacher: &guruID,
		updated: &entity.LearningModule{
			ID: moduleID, ClassID: classID, AddedByGuruID: guruID, MeetingNumber: 1,
			Title: "Judul Awal", Description: &desc, Status: constants.ModuleUpcoming,
			CreatedAt: now, UpdatedAt: now,
		},
		deleted: true,
	}
	u := &ClassroomUseCase{repo: repo, now: func() time.Time { return now.Add(time.Hour) }}

	_, err := u.UpdateModule(ctx, uuid.New(), classID, moduleID, dto.UpdateModuleRequest{})
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "VALIDATION_ERROR", appErr.Code)

	emptyTitle := "   "
	_, err = u.UpdateModule(ctx, uuid.New(), classID, moduleID, dto.UpdateModuleRequest{Title: &emptyTitle})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "VALIDATION_ERROR", appErr.Code)

	newTitle := " Judul Baru "
	clearDesc := " "
	newStatus := constants.ModuleCompleted
	num := 2
	updated, err := u.UpdateModule(ctx, uuid.New(), classID, moduleID, dto.UpdateModuleRequest{
		MeetingNumber: &num,
		Title:         &newTitle,
		Description:   &clearDesc,
		Status:        &newStatus,
	})
	require.NoError(t, err)
	require.Equal(t, 2, updated.MeetingNumber)
	require.Equal(t, "Judul Baru", updated.Title)
	require.Nil(t, updated.Description)
	require.Equal(t, constants.ModuleCompleted, updated.Status)

	repo.updated = nil
	_, err = u.UpdateModule(ctx, uuid.New(), classID, moduleID, dto.UpdateModuleRequest{Title: &newTitle})
	require.ErrorIs(t, err, apperror.ErrModuleNotFound)

	require.NoError(t, u.DeleteModule(ctx, uuid.New(), classID, moduleID))

	repo.deleted = false
	require.ErrorIs(t, u.DeleteModule(ctx, uuid.New(), classID, moduleID), apperror.ErrModuleNotFound)
}
