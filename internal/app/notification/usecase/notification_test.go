package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
	"github.com/kiboozapp/kibooz-backend/internal/domain/entity"
	"github.com/kiboozapp/kibooz-backend/internal/notifier"
	"github.com/kiboozapp/kibooz-backend/internal/pagination"
)

type fakeNotifRepo struct {
	rows        []entity.Notification
	total       int
	unreadCount int
	markFound   bool
	err         error

	lastRow    *entity.Notification
	batchKind  string
	batchRef   string
	batchAt    time.Time
	batchItems []notifier.StudentNotice
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

func (f *fakeNotifRepo) CreateForStudentParents(
	_ context.Context, kind string, refType string, createdAt time.Time, notices []notifier.StudentNotice,
) error {
	f.batchKind, f.batchRef, f.batchAt, f.batchItems = kind, refType, createdAt, notices

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

// newEmitter builds the use case with a fixed clock and an error report that goes nowhere
func newEmitter(repo *fakeNotifRepo, now time.Time) *NotificationUseCase {
	u := NewNotificationUseCase(repo).(*NotificationUseCase)
	u.now = func() time.Time { return now }
	u.report = func(error) {}

	return u
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
	u := newEmitter(repo, now)

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
	u := newEmitter(repo, now)

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

func TestNotifyStudentParentsStoresTheWholeBatchInOneCall(t *testing.T) {
	now := time.Date(2026, 11, 25, 10, 0, 0, 123456789, time.UTC)
	repo := &fakeNotifRepo{}
	u := newEmitter(repo, now)

	notices := []notifier.StudentNotice{
		{StudentID: uuid.New(), Title: "T", Body: "B1", RefID: uuid.New()},
		{StudentID: uuid.New(), Title: "T", Body: "B2", RefID: uuid.New()},
	}

	u.NotifyStudentParents(context.Background(), constants.NotificationMoodLogged, "mood_log", notices)

	require.Equal(t, constants.NotificationMoodLogged, repo.batchKind)
	require.Equal(t, "mood_log", repo.batchRef)
	require.Equal(t, now.Truncate(time.Microsecond), repo.batchAt)
	require.Equal(t, notices, repo.batchItems)

	repo.batchItems = nil
	u.NotifyStudentParents(context.Background(), constants.NotificationMoodLogged, "mood_log", nil)
	require.Nil(t, repo.batchItems, "an empty batch does not reach the store")
}

func TestAFailedInsertIsCountedAndReportedAndNeverReturned(t *testing.T) {
	repo := &fakeNotifRepo{err: errors.New("db down")}

	var reported []error

	u := NewNotificationUseCase(repo).(*NotificationUseCase)
	u.report = func(err error) { reported = append(reported, err) }

	target := uuid.New()

	u.NotifyStudentParent(context.Background(), target, constants.NotificationMoodLogged, "T", "B", "mood_log", uuid.New())
	u.NotifyStudentParents(context.Background(), constants.NotificationMoodLogged, "mood_log",
		[]notifier.StudentNotice{{StudentID: target, Title: "T", Body: "B"}})

	require.Len(t, reported, 2)
	require.ErrorIs(t, reported[0], repo.err)
	require.ErrorContains(t, reported[1], "notify student parents")

	repo.err = nil
	u.NotifyClassParents(context.Background(), target, constants.NotificationVideoAdded, "T", "B", "video", uuid.New())

	require.Len(t, reported, 2, "a stored notification reports nothing")
	require.InDelta(t, 2, testutil.ToFloat64(u.results.WithLabelValues("failed")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(u.results.WithLabelValues("stored")), 0)
}
