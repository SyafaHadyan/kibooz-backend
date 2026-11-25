package usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/guidance"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

func (u *GuruUseCase) ClassHistory(
	ctx context.Context, userID uuid.UUID, classID *uuid.UUID, eventType string, page pagination.Params,
) (dto.GuruHistoryResponse, error) {
	if eventType != "" && eventType != "all" && eventType != "mood" && eventType != "guidance" && eventType != "video" {
		return dto.GuruHistoryResponse{}, apperror.Validation(map[string]string{"type": "must be one of all, mood, guidance, video"})
	}

	guru, err := u.repo.FindGuruByUserID(ctx, userID)
	if err != nil {
		return dto.GuruHistoryResponse{}, apperror.Internal(err)
	}

	if guru == nil {
		return dto.GuruHistoryResponse{}, apperror.ErrProfileNotFound
	}

	class, err := u.repo.FindTaughtClass(ctx, guru.ID, classID)
	if err != nil {
		return dto.GuruHistoryResponse{}, apperror.Internal(err)
	}

	if class == nil {
		return dto.GuruHistoryResponse{}, apperror.ErrClassNotFound
	}

	rows, total, err := u.repo.ListClassHistory(ctx, class.ID, eventType, page.Limit, page.Offset())
	if err != nil {
		return dto.GuruHistoryResponse{}, apperror.Internal(err)
	}

	events := make([]dto.GuruHistoryEvent, 0, len(rows))
	for i := range rows {
		events = append(events, toGuruHistoryEvent(class.ID, class.Name, rows[i]))
	}

	return dto.GuruHistoryResponse{
		Events: events,
		Page:   page.Page,
		Limit:  page.Limit,
		Total:  total,
	}, nil
}

func toGuruHistoryEvent(classID uuid.UUID, className string, row repository.GuruHistoryRow) dto.GuruHistoryEvent {
	event := dto.GuruHistoryEvent{
		ID:              row.ID,
		Type:            row.EventType,
		ClassID:         classID,
		ClassName:       className,
		StudentID:       row.StudentID,
		StudentName:     row.StudentName,
		ActorName:       row.ActorName,
		MoodType:        row.MoodType,
		ConfidenceScore: row.ConfidenceScore,
		Description:     row.Description,
		OccurredAt:      row.OccurredAt.UTC(),
	}

	switch row.EventType {
	case "mood":
		label := "Mood recorded"
		if row.MoodType != nil {
			if named, ok := constants.MoodLabels[*row.MoodType]; ok {
				label = named
			} else {
				label = string(*row.MoodType)
			}
		}

		if row.StudentName != nil && *row.StudentName != "" {
			event.Title = fmt.Sprintf("%s: %s", *row.StudentName, label)
		} else {
			event.Title = label
		}
	case "guidance":
		title := "Guidance applied"
		if row.GuidanceID != nil {
			if guide, ok := guidance.ByID(*row.GuidanceID); ok {
				title = guide.Title
			}
		}

		if row.StudentName != nil && *row.StudentName != "" {
			event.Title = fmt.Sprintf("%s: %s", *row.StudentName, title)
		} else {
			event.Title = title
		}
	case "video":
		if row.Title != nil && *row.Title != "" {
			event.Title = *row.Title
		} else {
			event.Title = "Learning video added"
		}
	}

	return event
}
