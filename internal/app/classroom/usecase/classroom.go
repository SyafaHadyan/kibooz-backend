// Package usecase holds the learning video and forum business rules
package usecase

import (
	"context"
	"errors"
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
	// UpdateVideo changes the details of a video, any teacher of the class may do it
	UpdateVideo(
		ctx context.Context, userID uuid.UUID, classID uuid.UUID, videoID uuid.UUID, req dto.UpdateVideoRequest,
	) (dto.Video, error)
	// DeleteVideo removes a video and the file behind it when it was uploaded and no other video uses it
	DeleteVideo(ctx context.Context, userID uuid.UUID, classID uuid.UUID, videoID uuid.UUID) error
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
	// UpdateThread lets the author of a thread change its title or text
	UpdateThread(
		ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, req dto.UpdateThreadRequest,
	) (dto.ForumThread, error)
	// DeleteThread removes a thread with its replies, the author and the teachers of the class may do it
	DeleteThread(ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID) error
	// UpdateReply lets the author of a reply change its text
	UpdateReply(
		ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, replyID uuid.UUID,
		req dto.UpdateReplyRequest,
	) (dto.ForumReply, error)
	// DeleteReply removes a reply, the author and the teachers of the class may do it
	DeleteReply(
		ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, replyID uuid.UUID,
	) error
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

	// a file that was just uploaded waits under pending/ and is copied to its permanent key when the video is added,
	// so the bucket can expire whatever is never added without touching a real video
	submittedURL := video.VideoURL
	stagedKey := u.stagedVideoKey(classID, submittedURL)

	if stagedKey != "" {
		video.VideoURL = u.storage.PublicURL(strings.TrimPrefix(stagedKey, pendingPrefix))
	}

	err = u.withFileLock(ctx, video.VideoURL, func(repo repository.ClassroomDBItf) error {
		// the file is checked and the video stored under one lock, so the file cannot be deleted in between
		object, checkErr := u.checkUploadedVideo(ctx, classID, submittedURL)
		if checkErr != nil {
			return checkErr
		}

		if stagedKey == "" {
			return repo.CreateVideo(ctx, video)
		}

		permanentKey := strings.TrimPrefix(stagedKey, pendingPrefix)

		copyErr := u.storage.Copy(ctx, stagedKey, permanentKey, object.ContentType)
		if copyErr != nil {
			return copyErr
		}

		createErr := repo.CreateVideo(ctx, video)
		if createErr != nil {
			// nothing points at the copy, and the staged file expires by itself
			s3.Discard(ctx, u.storage, permanentKey)
		}

		return createErr
	})
	if err != nil {
		return dto.Video{}, asAppError(err)
	}

	if stagedKey != "" {
		// the staged file is only a leftover now, and the bucket removes it anyway if this fails
		s3.Discard(ctx, u.storage, stagedKey)
	}

	return videoResponse(video), nil
}

// stagedVideoKey returns the key of a video address that is a file this class uploaded and has not added yet, or "" for any other address
func (u *ClassroomUseCase) stagedVideoKey(classID uuid.UUID, videoURL string) string {
	key, ours := u.storage.KeyFromURL(videoURL)
	if ours && strings.HasPrefix(key, pendingVideoKeyPrefix(classID)) {
		return key
	}

	return ""
}

// withFileLock runs fn under the lock of an uploaded file, and without a lock for a link to another site
func (u *ClassroomUseCase) withFileLock(ctx context.Context, videoURL string, fn func(repo repository.ClassroomDBItf) error) error {
	_, ours := u.storage.KeyFromURL(videoURL)
	if !ours {
		return fn(u.repo)
	}

	return u.repo.WithVideoFileLock(ctx, videoURL, fn)
}

// asAppError keeps a typed error as it is and turns anything else into an internal error
func asAppError(err error) error {
	var appErr *apperror.Error

	if errors.As(err, &appErr) {
		return err
	}

	return apperror.Internal(err)
}

func (u *ClassroomUseCase) UpdateVideo(
	ctx context.Context, userID uuid.UUID, classID uuid.UUID, videoID uuid.UUID, req dto.UpdateVideoRequest,
) (dto.Video, error) {
	_, err := u.authorize(ctx, userID, constants.RoleGuru, classID)
	if err != nil {
		return dto.Video{}, err
	}

	if req.Title == nil && req.Description == nil && req.ThumbnailURL == nil && req.DurationSeconds == nil {
		return dto.Video{}, apperror.Validation(map[string]string{"body": "provide title, description, thumbnailUrl or durationSeconds"})
	}

	video, err := u.repo.FindVideo(ctx, classID, videoID)
	if err != nil {
		return dto.Video{}, apperror.Internal(err)
	}

	if video == nil {
		return dto.Video{}, apperror.ErrVideoNotFound
	}

	err = applyVideoChanges(video, req)
	if err != nil {
		return dto.Video{}, err
	}

	err = u.repo.SaveVideoDetails(ctx, video)
	if err != nil {
		return dto.Video{}, apperror.Internal(err)
	}

	return videoResponse(video), nil
}

func (u *ClassroomUseCase) DeleteVideo(ctx context.Context, userID uuid.UUID, classID uuid.UUID, videoID uuid.UUID) error {
	_, err := u.authorize(ctx, userID, constants.RoleGuru, classID)
	if err != nil {
		return err
	}

	video, err := u.repo.FindVideo(ctx, classID, videoID)
	if err != nil {
		return apperror.Internal(err)
	}

	if video == nil {
		return apperror.ErrVideoNotFound
	}

	// the video, the count and the file are handled under one lock, so no new video can be added for a file that is about to go
	err = u.withFileLock(ctx, video.VideoURL, func(repo repository.ClassroomDBItf) error {
		deleted, deleteErr := repo.DeleteVideo(ctx, classID, videoID)
		if deleteErr != nil {
			return deleteErr
		}

		// another request removed it first
		if !deleted {
			return apperror.ErrVideoNotFound
		}

		u.removeUploadedFile(ctx, repo, video.VideoURL)

		return nil
	})
	if err != nil {
		return asAppError(err)
	}

	return nil
}

// removeUploadedFile deletes the file of an uploaded video once no video points at it any more.
// A file that stays behind is only unused storage, so a failure here never fails the request.
func (u *ClassroomUseCase) removeUploadedFile(ctx context.Context, repo repository.ClassroomDBItf, videoURL string) {
	key, ours := u.storage.KeyFromURL(videoURL)
	if !ours {
		return
	}

	remaining, err := repo.CountVideosByURL(ctx, videoURL)
	if err != nil || remaining > 0 {
		return
	}

	s3.Discard(ctx, u.storage, key)
}

// applyVideoChanges checks the changed fields and writes them to the video
func applyVideoChanges(video *entity.LearningVideo, req dto.UpdateVideoRequest) error {
	details := map[string]string{}

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			details["title"] = "cannot be empty"
		} else {
			video.Title = title
		}
	}

	if req.Description != nil {
		video.Description = optional(*req.Description)
	}

	if req.ThumbnailURL != nil {
		thumbnail := optional(*req.ThumbnailURL)
		if thumbnail != nil && !isHTTPSURL(*thumbnail) {
			details["thumbnailUrl"] = "must be an https address"
		} else {
			video.ThumbnailURL = thumbnail
		}
	}

	if req.DurationSeconds != nil {
		video.DurationSeconds = nil
		if *req.DurationSeconds > 0 {
			video.DurationSeconds = req.DurationSeconds
		}
	}

	if len(details) > 0 {
		return apperror.Validation(details)
	}

	return nil
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

	// every file gets its own key under the class, so a later registration can tell which class it was made for.
	// It waits under pending/ until the video is added.
	key := fmt.Sprintf("%s%s%s", pendingVideoKeyPrefix(classID), uuid.New(), videoTypes[req.ContentType])
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
// either one that waits under pending/ or one that was added before. It returns the stored file, and nil for the teacher's own
// link to another site, which is left alone.
func (u *ClassroomUseCase) checkUploadedVideo(ctx context.Context, classID uuid.UUID, videoURL string) (*s3.Object, error) {
	key, ours := u.storage.KeyFromURL(videoURL)
	if !ours {
		return nil, nil
	}

	if !strings.HasPrefix(key, videoKeyPrefix(classID)) && !strings.HasPrefix(key, pendingVideoKeyPrefix(classID)) {
		return nil, apperror.Validation(map[string]string{"videoUrl": "is not a file uploaded for this class"})
	}

	object, err := u.storage.Stat(ctx, key)
	if err != nil {
		return nil, err
	}

	if object == nil {
		return nil, apperror.Validation(map[string]string{"videoUrl": "has no uploaded file yet"})
	}

	if _, accepted := videoTypes[object.ContentType]; !accepted || object.Size > int64(u.cfg.VideoMaxMB)*bytesPerMB {
		return nil, apperror.Validation(map[string]string{"videoUrl": "is not an accepted video file"})
	}

	return object, nil
}

// pendingPrefix holds the files that were uploaded and not added to a class yet, and the bucket expires it after a day
const pendingPrefix = "pending/"

func videoKeyPrefix(classID uuid.UUID) string {
	return "videos/" + classID.String() + "/"
}

func pendingVideoKeyPrefix(classID uuid.UUID) string {
	return pendingPrefix + videoKeyPrefix(classID)
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

func (u *ClassroomUseCase) UpdateThread(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, req dto.UpdateThreadRequest,
) (dto.ForumThread, error) {
	post, err := u.findOwnPost(ctx, userID, role, classID, nil, threadID)
	if err != nil {
		return dto.ForumThread{}, err
	}

	if req.Title == nil && req.Body == nil {
		return dto.ForumThread{}, apperror.Validation(map[string]string{"body": "provide title or body"})
	}

	details := map[string]string{}
	changed := post.ForumPost

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			details["title"] = "cannot be empty"
		}

		changed.Title = &title
	}

	if req.Body != nil {
		text := strings.TrimSpace(*req.Body)
		if text == "" {
			details["body"] = "cannot be empty"
		}

		changed.Body = text
	}

	if len(details) > 0 {
		return dto.ForumThread{}, apperror.Validation(details)
	}

	row, err := u.saveChanges(ctx, &changed)
	if err != nil {
		return dto.ForumThread{}, err
	}

	return threadResponse(row), nil
}

func (u *ClassroomUseCase) UpdateReply(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, replyID uuid.UUID,
	req dto.UpdateReplyRequest,
) (dto.ForumReply, error) {
	post, err := u.findOwnPost(ctx, userID, role, classID, &threadID, replyID)
	if err != nil {
		return dto.ForumReply{}, err
	}

	text := strings.TrimSpace(req.Body)
	if text == "" {
		return dto.ForumReply{}, apperror.Validation(map[string]string{"body": "cannot be empty"})
	}

	changed := post.ForumPost
	changed.Body = text

	row, err := u.saveChanges(ctx, &changed)
	if err != nil {
		return dto.ForumReply{}, err
	}

	return replyResponse(row), nil
}

func (u *ClassroomUseCase) DeleteThread(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID,
) error {
	return u.removePost(ctx, userID, role, classID, nil, threadID)
}

func (u *ClassroomUseCase) DeleteReply(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID uuid.UUID, replyID uuid.UUID,
) error {
	return u.removePost(ctx, userID, role, classID, &threadID, replyID)
}

// removePost deletes a post of the class for its author or for a teacher of the class
func (u *ClassroomUseCase) removePost(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID *uuid.UUID, postID uuid.UUID,
) error {
	post, guruID, err := u.findPostInClass(ctx, userID, role, classID, threadID, postID)
	if err != nil {
		return err
	}

	if post.AuthorUserID != userID && guruID == nil {
		return apperror.ErrForbidden
	}

	deleted, err := u.repo.DeletePost(ctx, postID)
	if err != nil {
		return apperror.Internal(err)
	}

	// another request removed it first
	if !deleted {
		return apperror.ErrForumPostNotFound
	}

	return nil
}

// findOwnPost returns a post that the account wrote, anyone else gets a refusal
func (u *ClassroomUseCase) findOwnPost(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID *uuid.UUID, postID uuid.UUID,
) (*repository.PostRow, error) {
	post, _, err := u.findPostInClass(ctx, userID, role, classID, threadID, postID)
	if err != nil {
		return nil, err
	}

	if post.AuthorUserID != userID {
		return nil, apperror.ErrForbidden
	}

	return post, nil
}

// findPostInClass checks that the account belongs to the class and that the post is a thread of it,
// or a reply to the given thread when threadID is set. It returns the guru id of a teacher.
func (u *ClassroomUseCase) findPostInClass(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID uuid.UUID, threadID *uuid.UUID, postID uuid.UUID,
) (*repository.PostRow, *uuid.UUID, error) {
	guruID, err := u.authorize(ctx, userID, role, classID)
	if err != nil {
		return nil, nil, err
	}

	post, err := u.repo.FindPost(ctx, postID)
	if err != nil {
		return nil, nil, apperror.Internal(err)
	}

	if post == nil || post.ClassID != classID || !postHasParent(post, threadID) {
		return nil, nil, apperror.ErrForumPostNotFound
	}

	return post, guruID, nil
}

// postHasParent tells whether a post is a thread when threadID is nil, and a reply to that thread otherwise
func postHasParent(post *repository.PostRow, threadID *uuid.UUID) bool {
	if threadID == nil {
		return post.ParentID == nil
	}

	return post.ParentID != nil && *post.ParentID == *threadID
}

func (u *ClassroomUseCase) saveChanges(ctx context.Context, post *entity.ForumPost) (*repository.PostRow, error) {
	err := u.repo.UpdatePost(ctx, post)
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
