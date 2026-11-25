// Package usecase implements in-app notification inbox retrieval, read tracking, and event emission.
package usecase

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/notification/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/notifier"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

type NotificationUseCaseItf interface {
	notifier.Notifier

	List(ctx context.Context, userID uuid.UUID, unreadOnly bool, page pagination.Params) (dto.NotificationList, error)
	MarkRead(ctx context.Context, userID uuid.UUID, notificationID uuid.UUID) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
}

type NotificationUseCase struct {
	repo repository.NotificationDBItf
	now  func() time.Time
}

func NewNotificationUseCase(repo repository.NotificationDBItf) NotificationUseCaseItf {
	return &NotificationUseCase{repo: repo, now: time.Now}
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
	if err != nil {
		log.Printf("notify student parent failed %v", err)
	}
}

func (u *NotificationUseCase) NotifyMoveRequestParent(
	ctx context.Context, requestID uuid.UUID, kind string, title string, body string,
) {
	err := u.repo.CreateForMoveRequestParent(ctx, u.buildRow(kind, title, body, "move_request", requestID), requestID)
	if err != nil {
		log.Printf("notify move request parent failed %v", err)
	}
}

func (u *NotificationUseCase) NotifyClassTeachers(
	ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID,
) {
	err := u.repo.CreateForClassTeachers(ctx, u.buildRow(kind, title, body, refType, refID), classID)
	if err != nil {
		log.Printf("notify class teachers failed %v", err)
	}
}

func (u *NotificationUseCase) NotifyClassParents(
	ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID,
) {
	err := u.repo.CreateForClassParents(ctx, u.buildRow(kind, title, body, refType, refID), classID)
	if err != nil {
		log.Printf("notify class parents failed %v", err)
	}
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
