package usecase

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// An archived class is read only, so every change to its forum and its videos is refused whoever asks, and nothing is stored.
func TestAnArchivedClassRefusesEveryChangeOfItsForumAndVideos(t *testing.T) {
	ctx := context.Background()
	classID, threadID, replyID, author, guruID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	title := "Title"

	repo := &forumRepo{
		teacher: &guruID, parent: true, archived: true,
		post: &repository.PostRow{ForumPost: entity.ForumPost{ID: threadID, ClassID: classID, AuthorUserID: author, Title: &title, Body: "Text"}},
	}
	u := &ClassroomUseCase{repo: repo}
	body := "new words"

	_, err := u.CreateThread(ctx, author, constants.RoleWali, classID, dto.CreateThreadRequest{Title: "T", Body: "B"})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	_, err = u.CreateReply(ctx, author, constants.RoleWali, classID, threadID, dto.CreateReplyRequest{Body: "B"})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	_, err = u.UpdateThread(ctx, author, constants.RoleWali, classID, threadID, dto.UpdateThreadRequest{Body: &body})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	_, err = u.UpdateReply(ctx, author, constants.RoleWali, classID, threadID, replyID, dto.UpdateReplyRequest{Body: body})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	require.ErrorIs(t, u.DeleteThread(ctx, author, constants.RoleWali, classID, threadID), apperror.ErrClassArchived)
	require.ErrorIs(t, u.DeleteReply(ctx, author, constants.RoleWali, classID, threadID, replyID), apperror.ErrClassArchived)

	_, err = u.AddVideo(ctx, author, classID, dto.AddVideoRequest{Title: "T", VideoURL: "https://example.com/v.mp4"})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	_, err = u.UpdateVideo(ctx, author, classID, uuid.New(), dto.UpdateVideoRequest{})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	require.ErrorIs(t, u.DeleteVideo(ctx, author, classID, uuid.New()), apperror.ErrClassArchived)
	require.ErrorIs(t, u.DiscardVideoUpload(ctx, author, classID, "https://example.com/v.mp4"), apperror.ErrClassArchived)

	_, err = u.CreateVideoUpload(ctx, author, classID, dto.VideoUploadRequest{})
	require.ErrorIs(t, err, apperror.ErrClassArchived)
}

func TestAnArchivedClassStillAnswersWhoMayNotWrite(t *testing.T) {
	ctx := context.Background()
	classID := uuid.New()
	repo := &forumRepo{archived: true}
	u := &ClassroomUseCase{repo: repo}

	// a person who is not in the class is told so first, whether or not the class is archived
	_, err := u.CreateThread(ctx, uuid.New(), constants.RoleWali, classID, dto.CreateThreadRequest{Title: "T", Body: "B"})
	require.ErrorIs(t, err, apperror.ErrForbidden)
}
