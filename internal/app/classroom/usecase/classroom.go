// Package usecase holds the learning video and forum business rules
package usecase

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/clock"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

// DeletedAuthorName stands in for the name of an account that was deleted after it wrote a post
const DeletedAuthorName = "Deleted account"

type ClassroomUseCaseItf interface {
	ListVideos(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, page pagination.Params) (dto.VideoList, error)
	AddVideo(ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.AddVideoRequest) (dto.Video, error)
	// CreateVideoUpload signs a URL the teacher sends a video file to, the file is then registered with AddVideo
	CreateVideoUpload(ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.VideoUploadRequest) (dto.VideoUpload, error)
	ListThreads(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, page pagination.Params) (dto.ForumThreadList, error)
	CreateThread(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, req dto.CreateThreadRequest) (dto.ForumThread, error)
	ListReplies(
		ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, page pagination.Params,
	) (dto.ForumReplyList, error)
	CreateReply(
		ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, req dto.CreateReplyRequest,
	) (dto.ForumReply, error)
	// ListStudents is for the teachers of the class, it shows each child with the latest mood of today
	ListStudents(ctx context.Context, userID uuid.UUID, classID uuid.UUID, page pagination.Params) (dto.ClassStudentList, error)
}

// videoTypes maps each accepted video type to the extension of its object key
var videoTypes = map[string]string{"video/mp4": ".mp4", "video/webm": ".webm"}

const bytesPerMB = 1024 * 1024

type ClassroomUseCase struct {
	repo    repository.ClassroomDBItf
	storage s3.StorageItf
	cfg     *env.Env
	now     func() time.Time
}

func NewClassroomUseCase(repo repository.ClassroomDBItf, storage s3.StorageItf, cfg *env.Env) ClassroomUseCaseItf {
	return &ClassroomUseCase{repo: repo, storage: storage, cfg: cfg, now: time.Now}
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

	err = u.checkUploadedVideo(ctx, classID, video.VideoURL)
	if err != nil {
		return dto.Video{}, err
	}

	err = u.repo.CreateVideo(ctx, video)
	if err != nil {
		return dto.Video{}, apperror.Internal(err)
	}

	return videoResponse(video), nil
}

func (u *ClassroomUseCase) CreateVideoUpload(
	ctx context.Context, userID uuid.UUID, classID uuid.UUID, req dto.VideoUploadRequest,
) (dto.VideoUpload, error) {
	_, err := u.authorize(ctx, userID, constants.RoleGuru, classID)
	if err != nil {
		return dto.VideoUpload{}, err
	}

	maxBytes := int64(u.cfg.VideoMaxMB) * bytesPerMB
	if req.SizeBytes > maxBytes {
		return dto.VideoUpload{}, apperror.Validation(map[string]string{
			"sizeBytes": fmt.Sprintf("must be at most %d bytes", maxBytes),
		})
	}

	// every file gets its own key under the class, so a later registration can tell which class it was made for
	key := fmt.Sprintf("%s%s%s", videoKeyPrefix(classID), uuid.New(), videoTypes[req.ContentType])
	ttl := time.Duration(u.cfg.VideoUploadURLSeconds) * time.Second

	uploadURL, err := u.storage.PresignUpload(ctx, key, req.ContentType, req.SizeBytes, ttl)
	if err != nil {
		return dto.VideoUpload{}, err
	}

	return dto.VideoUpload{
		UploadURL: uploadURL,
		Method:    http.MethodPut,
		Headers:   map[string]string{"Content-Type": req.ContentType},
		VideoURL:  u.storage.PublicURL(key),
		ExpiresAt: u.now().Add(ttl).UTC(),
	}, nil
}

// checkUploadedVideo makes sure a video address that points into our own bucket is a finished upload made for this class,
// any other address is the teacher's own link and is left alone
func (u *ClassroomUseCase) checkUploadedVideo(ctx context.Context, classID uuid.UUID, videoURL string) error {
	key, ours := u.storage.KeyFromURL(videoURL)
	if !ours {
		return nil
	}

	if !strings.HasPrefix(key, videoKeyPrefix(classID)) {
		return apperror.Validation(map[string]string{"videoUrl": "is not a file uploaded for this class"})
	}

	object, err := u.storage.Stat(ctx, key)
	if err != nil {
		return err
	}

	if object == nil {
		return apperror.Validation(map[string]string{"videoUrl": "has no uploaded file yet"})
	}

	if _, accepted := videoTypes[object.ContentType]; !accepted || object.Size > int64(u.cfg.VideoMaxMB)*bytesPerMB {
		return apperror.Validation(map[string]string{"videoUrl": "is not an accepted video file"})
	}

	return nil
}

func videoKeyPrefix(classID uuid.UUID) string {
	return "videos/" + classID.String() + "/"
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

func (u *ClassroomUseCase) ListStudents(
	ctx context.Context, userID uuid.UUID, classID uuid.UUID, page pagination.Params,
) (dto.ClassStudentList, error) {
	_, err := u.authorize(ctx, userID, constants.RoleGuru, classID)
	if err != nil {
		return dto.ClassStudentList{}, err
	}

	from, to := clock.DayBounds(u.now(), u.cfg.Location())

	rows, total, err := u.repo.ListStudents(ctx, classID, from, to, page.Limit, page.Offset())
	if err != nil {
		return dto.ClassStudentList{}, apperror.Internal(err)
	}

	res := dto.ClassStudentList{
		Students: make([]dto.ClassStudent, 0, len(rows)),
		PageInfo: dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range rows {
		res.Students = append(res.Students, dto.ClassStudent{
			ID:            rows[i].ID,
			FullName:      rows[i].FullName,
			NISN:          rows[i].NISN,
			AvatarURL:     rows[i].AvatarURL,
			CurrentPoints: rows[i].CurrentPoints,
			ClassRank:     rows[i].RankPosition,
			TodayMood:     rows[i].TodayMood,
		})
	}

	return res, nil
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
