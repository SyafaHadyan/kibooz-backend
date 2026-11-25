package usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/guidance"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

func (u *WaliUseCase) ChildHistory(
	ctx context.Context, userID uuid.UUID, studentID uuid.UUID, eventType string, page pagination.Params,
) (dto.WaliHistoryResponse, error) {
	if eventType != "" && eventType != "all" && eventType != "mood" && eventType != "trash" && eventType != "guidance" {
		return dto.WaliHistoryResponse{}, apperror.Validation(map[string]string{"type": "must be one of all, mood, trash, guidance"})
	}

	student, err := u.resolveStudent(ctx, userID, &studentID)
	if err != nil {
		return dto.WaliHistoryResponse{}, err
	}

	rows, total, err := u.repo.ListChildHistory(ctx, student.ID, eventType, page.Limit, page.Offset())
	if err != nil {
		return dto.WaliHistoryResponse{}, apperror.Internal(err)
	}

	events := make([]dto.WaliHistoryEvent, 0, len(rows))
	for i := range rows {
		events = append(events, toWaliHistoryEvent(rows[i]))
	}

	return dto.WaliHistoryResponse{
		Student: dto.WaliHistoryStudent{
			ID:        student.ID,
			FullName:  student.FullName,
			ClassName: student.ClassName,
		},
		Events: events,
		Page:   page.Page,
		Limit:  page.Limit,
		Total:  total,
	}, nil
}

func toWaliHistoryEvent(row repository.WaliHistoryRow) dto.WaliHistoryEvent {
	event := dto.WaliHistoryEvent{
		ID:            row.ID,
		Type:          row.EventType,
		MoodType:      row.MoodType,
		TrashType:     row.TrashType,
		PointsAwarded: row.PointsAwarded,
		GuidanceID:    row.GuidanceID,
		Description:   row.Notes,
		OccurredAt:    row.OccurredAt.UTC(),
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

		event.Title = fmt.Sprintf("Mood: %s", label)
	case "trash":
		if row.TrashType != nil && row.PointsAwarded != nil {
			event.Title = fmt.Sprintf("%s (+%d points)", *row.TrashType, *row.PointsAwarded)
		} else {
			event.Title = "Trash scan recorded"
		}
	case "guidance":
		if row.GuidanceID != nil {
			if guide, ok := guidance.ByID(*row.GuidanceID); ok {
				title := guide.Title
				category := guide.Category
				event.Title = title
				event.GuidanceTitle = &title
				event.GuidanceCategory = &category

				return event
			}
		}

		event.Title = "Guidance applied"
	}

	return event
}
