// Package usecase holds the learning video and forum business rules
package usecase

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

// DeletedAuthorName stands in for the name of an account that was deleted after it wrote a post
const DeletedAuthorName = "Deleted account"

type ClassroomUseCaseItf interface {
	ListVideos(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, page pagination.Params) (dto.VideoList, error)
	AddVideo(ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.AddVideoRequest) (dto.Video, error)
	ListThreads(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, page pagination.Params) (dto.ForumThreadList, error)
	CreateThread(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, req dto.CreateThreadRequest) (dto.ForumThread, error)
	ListReplies(
		ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, page pagination.Params,
	) (dto.ForumReplyList, error)
	CreateReply(
		ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, req dto.CreateReplyRequest,
	) (dto.ForumReply, error)
}

type ClassroomUseCase struct {
	repo repository.ClassroomDBItf
	now  func() time.Time
}

func NewClassroomUseCase(repo repository.ClassroomDBItf) ClassroomUseCaseItf {
	return &ClassroomUseCase{repo: repo, now: time.Now}
}

func (u *ClassroomUseCase) ListVideos(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, page pagination.Params,
) (dto.VideoList, error) {
	_, err := u.authorize(ctx, userID, role, classID)
	if err != nil {
		return dto.VideoList{}, err
	}

	videos, total, err := u.repo.ListVideos(ctx, classID, page.Limit, page.Offset())
	if err != nil {
		return dto.VideoList{}, apperror.Internal(err)
	}

	res := dto.VideoList{
		Videos:   make([]dto.Video, 0, len(videos)),
		PageInfo: dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range videos {
		res.Videos = append(res.Videos, videoResponse(&videos[i]))
	}

	return res, nil
}

func (u *ClassroomUseCase) AddVideo(ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.AddVideoRequest) (dto.Video, error) {
	guruID, err := u.authorize(ctx, userID, constants.RoleGuru, classID)
	if err != nil {
		return dto.Video{}, err
	}

	video := &entity.LearningVideo{
		ID:              uuid.New(),
		ClassID:         classID,
		AddedByGuruID:   *guruID,
		Title:           strings.TrimSpace(req.Title),
		VideoURL:        strings.TrimSpace(req.VideoURL),
		Description:     optional(req.Description),
		ThumbnailURL:    optional(req.ThumbnailURL),
		DurationSeconds: req.DurationSeconds,
		CreatedAt:       u.timestamp(),
	}

	details := map[string]string{}

	if video.Title == "" {
		details["title"] = "is required"
	}

	if !isHTTPSURL(video.VideoURL) {
		details["videoUrl"] = "must be an https address"
	}

	if video.ThumbnailURL != nil && !isHTTPSURL(*video.ThumbnailURL) {
		details["thumbnailUrl"] = "must be an https address"
	}

	if len(details) > 0 {
		return dto.Video{}, apperror.Validation(details)
	}

	err = u.repo.CreateVideo(ctx, video)
	if err != nil {
		return dto.Video{}, apperror.Internal(err)
	}

	return videoResponse(video), nil
}

func (u *ClassroomUseCase) ListThreads(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, page pagination.Params,
) (dto.ForumThreadList, error) {
	_, err := u.authorize(ctx, userID, role, classID)
	if err != nil {
		return dto.ForumThreadList{}, err
	}

	rows, total, err := u.repo.ListThreads(ctx, classID, page.Limit, page.Offset())
	if err != nil {
		return dto.ForumThreadList{}, apperror.Internal(err)
	}

	res := dto.ForumThreadList{
		Threads:  make([]dto.ForumThread, 0, len(rows)),
		PageInfo: dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range rows {
		res.Threads = append(res.Threads, threadResponse(&rows[i]))
	}

	return res, nil
}

func (u *ClassroomUseCase) CreateThread(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, req dto.CreateThreadRequest,
) (dto.ForumThread, error) {
	_, err := u.authorize(ctx, userID, role, classID)
	if err != nil {
		return dto.ForumThread{}, err
	}

	title, body := strings.TrimSpace(req.Title), strings.TrimSpace(req.Body)

	details := map[string]string{}

	if title == "" {
		details["title"] = "is required"
	}

	if body == "" {
		details["body"] = "is required"
	}

	if len(details) > 0 {
		return dto.ForumThread{}, apperror.Validation(details)
	}

	post, err := u.store(ctx, &entity.ForumPost{
		ID:           uuid.New(),
		ClassID:      classID,
		AuthorUserID: userID,
		Title:        &title,
		Body:         body,
		CreatedAt:    u.timestamp(),
	})
	if err != nil {
		return dto.ForumThread{}, err
	}

	return threadResponse(post), nil
}

func (u *ClassroomUseCase) ListReplies(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, page pagination.Params,
) (dto.ForumReplyList, error) {
	err := u.authorizeThread(ctx, userID, role, classID, threadID)
	if err != nil {
		return dto.ForumReplyList{}, err
	}

	rows, total, err := u.repo.ListReplies(ctx, threadID, page.Limit, page.Offset())
	if err != nil {
		return dto.ForumReplyList{}, apperror.Internal(err)
	}

	res := dto.ForumReplyList{
		Replies:  make([]dto.ForumReply, 0, len(rows)),
		PageInfo: dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range rows {
		res.Replies = append(res.Replies, replyResponse(&rows[i]))
	}

	return res, nil
}

func (u *ClassroomUseCase) CreateReply(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, req dto.CreateReplyRequest,
) (dto.ForumReply, error) {
	err := u.authorizeThread(ctx, userID, role, classID, threadID)
	if err != nil {
		return dto.ForumReply{}, err
	}

	body := strings.TrimSpace(req.Body)
	if body == "" {
		return dto.ForumReply{}, apperror.Validation(map[string]string{"body": "is required"})
	}

	post, err := u.store(ctx, &entity.ForumPost{
		ID:           uuid.New(),
		ClassID:      classID,
		ParentID:     &threadID,
		AuthorUserID: userID,
		Body:         body,
		CreatedAt:    u.timestamp(),
	})
	if err != nil {
		return dto.ForumReply{}, err
	}

	return replyResponse(post), nil
}

// authorize lets a teacher of the class and a parent of a child in it through. It returns the guru id for a teacher.
func (u *ClassroomUseCase) authorize(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID) (*uuid.UUID, error) {
	switch role {
	case constants.RoleGuru:
		guruID, err := u.repo.TeacherOfClass(ctx, userID, classID)
		if err != nil {
			return nil, apperror.Internal(err)
		}

		if guruID == nil {
			return nil, apperror.ErrForbidden
		}

		return guruID, nil
	case constants.RoleWali:
		member, err := u.repo.ParentInClass(ctx, userID, classID)
		if err != nil {
			return nil, apperror.Internal(err)
		}

		if !member {
			return nil, apperror.ErrForbidden
		}

		return nil, nil
	default:
		return nil, apperror.ErrForbidden
	}
}

// authorizeThread also confirms that the thread belongs to the class
func (u *ClassroomUseCase) authorizeThread(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID,
) error {
	_, err := u.authorize(ctx, userID, role, classID)
	if err != nil {
		return err
	}

	exists, err := u.repo.ThreadExists(ctx, classID, threadID)
	if err != nil {
		return apperror.Internal(err)
	}

	if !exists {
		return apperror.ErrForumPostNotFound
	}

	return nil
}

// store saves a post and reads it back together with its author
func (u *ClassroomUseCase) store(ctx context.Context, post *entity.ForumPost) (*repository.PostRow, error) {
	err := u.repo.CreatePost(ctx, post)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	row, err := u.repo.FindPost(ctx, post.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if row == nil {
		return nil, apperror.ErrForumPostNotFound
	}

	return row, nil
}

// timestamp keeps the precision PostgreSQL stores, so the order of two quick posts is stable
func (u *ClassroomUseCase) timestamp() time.Time {
	return u.now().UTC().Truncate(time.Microsecond)
}

func videoResponse(video *entity.LearningVideo) dto.Video {
	return dto.Video{
		ID:              video.ID,
		Title:           video.Title,
		Description:     video.Description,
		VideoURL:        video.VideoURL,
		ThumbnailURL:    video.ThumbnailURL,
		DurationSeconds: video.DurationSeconds,
		CreatedAt:       video.CreatedAt.UTC(),
	}
}

func threadResponse(row *repository.PostRow) dto.ForumThread {
	title := ""
	if row.Title != nil {
		title = *row.Title
	}

	return dto.ForumThread{
		ID:         row.ID,
		Title:      title,
		Body:       row.Body,
		Author:     authorResponse(row),
		ReplyCount: row.ReplyCount,
		CreatedAt:  row.CreatedAt.UTC(),
	}
}

func replyResponse(row *repository.PostRow) dto.ForumReply {
	return dto.ForumReply{
		ID:        row.ID,
		Body:      row.Body,
		Author:    authorResponse(row),
		CreatedAt: row.CreatedAt.UTC(),
	}
}

// authorResponse hides the name and photo of an account that was deleted
func authorResponse(row *repository.PostRow) dto.ForumAuthor {
	author := dto.ForumAuthor{ID: row.AuthorUserID, FullName: row.AuthorName, Role: row.AuthorRole, AvatarURL: row.AuthorAvatarURL}

	if row.AuthorDeleted {
		author.FullName = DeletedAuthorName
		author.AvatarURL = nil
	}

	return author
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return &value
}

// isHTTPSURL accepts an absolute https address without credentials or whitespace
func isHTTPSURL(raw string) bool {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	return parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil
}
