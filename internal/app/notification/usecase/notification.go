// Package usecase implements in-app notification inbox retrieval, read tracking, and event emission.
package usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kiboozapp/kibooz-backend/internal/app/notification/repository"
	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/domain/entity"
	"github.com/kiboozapp/kibooz-backend/internal/errorreport"
	"github.com/kiboozapp/kibooz-backend/internal/notifier"
	"github.com/kiboozapp/kibooz-backend/internal/pagination"
)

type NotificationUseCaseItf interface {
	notifier.Notifier

	// Collectors are the metrics of the emitter
	Collectors() []prometheus.Collector
	List(ctx context.Context, userID uuid.UUID, unreadOnly bool, page pagination.Params) (dto.NotificationList, error)
	MarkRead(ctx context.Context, userID uuid.UUID, notificationID uuid.UUID) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
}

type NotificationUseCase struct {
	repo    repository.NotificationDBItf
	now     func() time.Time
	results *prometheus.CounterVec
	// report sends a failure to the error reports, a field for the tests
	report func(error)
}

func NewNotificationUseCase(repo repository.NotificationDBItf) NotificationUseCaseItf {
	return &NotificationUseCase{
		repo: repo, now: time.Now,
		results: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "notifications_emitted_total", Help: "Notification inserts after an event, by result.",
		}, []string{"result"}),
		report: func(err error) { errorreport.Report(err, errorreport.Info{}) },
	}
}

// Collectors are the metrics of the emitter
func (u *NotificationUseCase) Collectors() []prometheus.Collector {
	// the series exist from the start, so a result that never happened still shows
	for _, result := range []string{"stored", "failed"} {
		u.results.WithLabelValues(result)
	}

	return []prometheus.Collector{u.results}
}

// emitted counts the result of one insert. A failure is logged, counted and reported, and never fails the action that caused it,
// because a notification is a shortcut to what the action already stored.
func (u *NotificationUseCase) emitted(what string, err error) {
	if err == nil {
		u.results.WithLabelValues("stored").Inc()

		return
	}

	u.results.WithLabelValues("failed").Inc()
	log.Printf("%s failed %v", what, err)
	u.report(fmt.Errorf("%s: %w", what, err))
}

func (u *NotificationUseCase) List(
	ctx context.Context, userID uuid.UUID, unreadOnly bool, page pagination.Params,
) (dto.NotificationList, error) {
	rows, total, unreadCount, err := u.repo.List(ctx, userID, unreadOnly, page.Limit, page.Offset())
	if err != nil {
		return dto.NotificationList{}, apperror.Internal(err)
	}

	res := dto.NotificationList{
		Notifications: make([]dto.Notification, 0, len(rows)),
		UnreadCount:   unreadCount,
		PageInfo:      dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range rows {
		res.Notifications = append(res.Notifications, notificationResponse(&rows[i]))
	}

	return res, nil
}

func (u *NotificationUseCase) MarkRead(ctx context.Context, userID uuid.UUID, notificationID uuid.UUID) error {
	found, err := u.repo.MarkRead(ctx, userID, notificationID, u.timestamp())
	if err != nil {
		return apperror.Internal(err)
	}

	if !found {
		return apperror.ErrNotificationNotFound
	}

	return nil
}

func (u *NotificationUseCase) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	err := u.repo.MarkAllRead(ctx, userID, u.timestamp())
	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (u *NotificationUseCase) NotifyStudentParent(
	ctx context.Context, studentID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID,
) {
	err := u.repo.CreateForStudentParent(ctx, u.buildRow(kind, title, body, refType, refID), studentID)
	u.emitted("notify student parent", err)
}

func (u *NotificationUseCase) NotifyStudentParents(
	ctx context.Context, kind string, refType string, notices []notifier.StudentNotice,
) {
	if len(notices) == 0 {
		return
	}

	err := u.repo.CreateForStudentParents(ctx, kind, refType, u.timestamp(), notices)
	u.emitted("notify student parents", err)
}

func (u *NotificationUseCase) NotifyMoveRequestParent(
	ctx context.Context, requestID uuid.UUID, kind string, title string, body string,
) {
	err := u.repo.CreateForMoveRequestParent(ctx, u.buildRow(kind, title, body, "move_request", requestID), requestID)
	u.emitted("notify move request parent", err)
}

func (u *NotificationUseCase) NotifyClassTeachers(
	ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID,
) {
	err := u.repo.CreateForClassTeachers(ctx, u.buildRow(kind, title, body, refType, refID), classID)
	u.emitted("notify class teachers", err)
}

func (u *NotificationUseCase) NotifyClassParents(
	ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID,
) {
	err := u.repo.CreateForClassParents(ctx, u.buildRow(kind, title, body, refType, refID), classID)
	u.emitted("notify class parents", err)
}

func (u *NotificationUseCase) buildRow(kind string, title string, body string, refType string, refID uuid.UUID) *entity.Notification {
	row := &entity.Notification{
		Kind:      kind,
		Title:     title,
		Body:      body,
		CreatedAt: u.timestamp(),
	}

	if refType != "" {
		row.ReferenceType = &refType
	}

	if refID != uuid.Nil {
		row.ReferenceID = &refID
	}

	return row
}

func (u *NotificationUseCase) timestamp() time.Time {
	return u.now().UTC().Truncate(time.Microsecond)
}

func notificationResponse(row *entity.Notification) dto.Notification {
	res := dto.Notification{
		ID:            row.ID,
		Kind:          row.Kind,
		Title:         row.Title,
		Body:          row.Body,
		ReferenceType: row.ReferenceType,
		ReferenceID:   row.ReferenceID,
		CreatedAt:     row.CreatedAt.UTC(),
	}

	if row.ReadAt != nil {
		read := row.ReadAt.UTC()
		res.ReadAt = &read
	}

	return res
}
