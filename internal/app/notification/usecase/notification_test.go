package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

type fakeNotifRepo struct {
	rows        []entity.Notification
	total       int
	unreadCount int
	markFound   bool
	err         error

	lastRow    *entity.Notification
	lastTarget uuid.UUID
	markedID   uuid.UUID
	markedAt   time.Time
}

func (f *fakeNotifRepo) List(context.Context, uuid.UUID, bool, int, int) ([]entity.Notification, int, int, error) {
	return f.rows, f.total, f.unreadCount, f.err
}

func (f *fakeNotifRepo) MarkRead(_ context.Context, _ uuid.UUID, id uuid.UUID, readAt time.Time) (bool, error) {
	f.markedID, f.markedAt = id, readAt

	return f.markFound, f.err
}

func (f *fakeNotifRepo) MarkAllRead(_ context.Context, _ uuid.UUID, readAt time.Time) error {
	f.markedAt = readAt

	return f.err
}

func (f *fakeNotifRepo) CreateForStudentParent(_ context.Context, row *entity.Notification, studentID uuid.UUID) error {
	f.lastRow, f.lastTarget = row, studentID

	return f.err
}

func (f *fakeNotifRepo) CreateForMoveRequestParent(_ context.Context, row *entity.Notification, requestID uuid.UUID) error {
	f.lastRow, f.lastTarget = row, requestID

	return f.err
}

func (f *fakeNotifRepo) CreateForClassTeachers(_ context.Context, row *entity.Notification, classID uuid.UUID) error {
	f.lastRow, f.lastTarget = row, classID

	return f.err
}

func (f *fakeNotifRepo) CreateForClassParents(_ context.Context, row *entity.Notification, classID uuid.UUID) error {
	f.lastRow, f.lastTarget = row, classID

	return f.err
}

func TestNotificationListAndRead(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	refType := "mood_log"
	refID := uuid.New()
	notifID := uuid.New()

	repo := &fakeNotifRepo{
		total:       1,
		unreadCount: 1,
		markFound:   true,
		rows: []entity.Notification{{
			ID: notifID, UserID: uuid.New(), Kind: constants.NotificationMoodLogged,
			Title: "Daily mood recorded", Body: "Happy mood",
			ReferenceType: &refType, ReferenceID: &refID, CreatedAt: now,
		}},
	}
	u := &NotificationUseCase{repo: repo, now: func() time.Time { return now }}

	res, err := u.List(ctx, uuid.New(), false, pagination.Params{Page: 1, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, 1, res.UnreadCount)
	require.Len(t, res.Notifications, 1)
	require.Equal(t, notifID, res.Notifications[0].ID)

	require.NoError(t, u.MarkRead(ctx, uuid.New(), notifID))
	require.Equal(t, now, repo.markedAt)

	repo.markFound = false
	require.ErrorIs(t, u.MarkRead(ctx, uuid.New(), notifID), apperror.ErrNotificationNotFound)

	require.NoError(t, u.MarkAllRead(ctx, uuid.New()))
}

func TestNotificationEmittersAndErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	repo := &fakeNotifRepo{}
	u := &NotificationUseCase{repo: repo, now: func() time.Time { return now }}

	target := uuid.New()
	ref := uuid.New()

	u.NotifyStudentParent(ctx, target, constants.NotificationMoodLogged, "T", "B", "mood_log", ref)
	require.Equal(t, target, repo.lastTarget)
	require.Equal(t, "T", repo.lastRow.Title)

	u.NotifyMoveRequestParent(ctx, target, constants.NotificationMoveDecided, "T2", "B2")
	require.Equal(t, "move_request", *repo.lastRow.ReferenceType)

	u.NotifyClassTeachers(ctx, target, constants.NotificationGuidanceApplied, "T3", "B3", "guidance_application", ref)
	u.NotifyClassParents(ctx, target, constants.NotificationVideoAdded, "T4", "B4", "video", ref)

	repo.err = errors.New("db down")
	var appErr *apperror.Error
	_, err := u.List(ctx, uuid.New(), false, pagination.Params{Page: 1, Limit: 20})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)

	require.ErrorAs(t, u.MarkRead(ctx, uuid.New(), uuid.New()), &appErr)
	require.ErrorAs(t, u.MarkAllRead(ctx, uuid.New()), &appErr)

	// Notification emission errors never panic
	u.NotifyStudentParent(ctx, target, constants.NotificationMoodLogged, "T", "B", "", uuid.Nil)
	u.NotifyMoveRequestParent(ctx, target, constants.NotificationMoveDecided, "T", "B")
	u.NotifyClassTeachers(ctx, target, constants.NotificationGuidanceApplied, "T", "B", "", uuid.Nil)
	u.NotifyClassParents(ctx, target, constants.NotificationVideoAdded, "T", "B", "", uuid.Nil)
}
