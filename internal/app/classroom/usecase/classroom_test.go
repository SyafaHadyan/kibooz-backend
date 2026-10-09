package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

func TestIsHTTPSURL(t *testing.T) {
	tests := map[string]bool{
		"https://videos.example.com/watch?v=1":  true,
		"https://example.com":                   true,
		"https://example.com:8443/a/b.mp4":      true,
		"":                                      false,
		"http://example.com/a.mp4":              false,
		"ftp://example.com/a.mp4":               false,
		"javascript:alert(1)":                   false,
		"//example.com/a.mp4":                   false,
		"/videos/a.mp4":                         false,
		"https://":                              false,
		"https:///a.mp4":                        false,
		"https://user:secret@example.com/a.mp4": false,
		"https://example.com/a b.mp4":           false,
		"https://example.com/a\nb.mp4":          false,
		"https://exa mple.com/a.mp4":            false,
		"https://example.com/%zz":               false,
		"https://[::1:8080/":                    false,
	}

	for raw, want := range tests {
		require.Equal(t, want, isHTTPSURL(raw), raw)
	}
}

func TestOptionalDropsBlankText(t *testing.T) {
	require.Nil(t, optional(""))
	require.Nil(t, optional("  \n "))

	got := optional("  hello ")
	require.NotNil(t, got)
	require.Equal(t, "hello", *got)
}

func TestAuthorResponseHidesADeletedAccount(t *testing.T) {
	avatar := "https://cdn.example.test/avatars/a.png"
	row := &repository.PostRow{
		ForumPost:       entity.ForumPost{AuthorUserID: uuid.New()},
		AuthorName:      "Sarah Kartika",
		AuthorAvatarURL: &avatar,
		AuthorRole:      constants.RoleWali,
	}

	active := authorResponse(row)
	require.Equal(t, "Sarah Kartika", active.FullName)
	require.Equal(t, &avatar, active.AvatarURL)
	require.Equal(t, constants.RoleWali, active.Role)
	require.Equal(t, row.AuthorUserID, active.ID)

	row.AuthorDeleted = true

	deleted := authorResponse(row)
	require.Equal(t, DeletedAuthorName, deleted.FullName)
	require.Nil(t, deleted.AvatarURL)
	require.Equal(t, constants.RoleWali, deleted.Role)
	require.Equal(t, row.AuthorUserID, deleted.ID)
}

func TestThreadResponseHandlesAMissingTitle(t *testing.T) {
	require.Empty(t, threadResponse(&repository.PostRow{}).Title)

	title := "Field trip"
	row := &repository.PostRow{ForumPost: entity.ForumPost{Title: &title}, ReplyCount: 3}

	got := threadResponse(row)
	require.Equal(t, "Field trip", got.Title)
	require.Equal(t, 3, got.ReplyCount)
}

// bucket answers the storage questions of the uploaded video check from memory
type bucket struct {
	s3.Disabled

	objects map[string]*s3.Object
	failure error
}

func (b bucket) KeyFromURL(raw string) (string, bool) {
	return strings.CutPrefix(raw, "https://cdn.example.test/")
}

func (b bucket) PublicURL(key string) string {
	return "https://cdn.example.test/" + key
}

func (b bucket) Stat(_ context.Context, key string) (*s3.Object, error) {
	return b.objects[key], b.failure
}

func TestCheckUploadedVideo(t *testing.T) {
	classID := uuid.New()
	key := videoKeyPrefix(classID) + uuid.NewString() + ".mp4"
	staged := pendingVideoKeyPrefix(classID) + uuid.NewString() + ".mp4"
	other := videoKeyPrefix(uuid.New()) + uuid.NewString() + ".mp4"
	otherStaged := pendingVideoKeyPrefix(uuid.New()) + uuid.NewString() + ".mp4"

	good := &s3.Object{Size: 5 * bytesPerMB, ContentType: "video/mp4"}

	tests := map[string]struct {
		url     string
		bucket  bucket
		wantErr string
	}{
		"a link to another site":     {"https://videos.example.com/a.mp4", bucket{}, ""},
		"a finished upload":          {"https://cdn.example.test/" + key, bucket{objects: map[string]*s3.Object{key: good}}, ""},
		"a file waiting to be added": {"https://cdn.example.test/" + staged, bucket{objects: map[string]*s3.Object{staged: good}}, ""},
		"a file of another class":    {"https://cdn.example.test/" + other, bucket{objects: map[string]*s3.Object{other: good}}, "is not a file uploaded for this class"},
		"a waiting file of another class": {
			"https://cdn.example.test/" + otherStaged, bucket{objects: map[string]*s3.Object{otherStaged: good}}, "is not a file uploaded for this class",
		},
		"an avatar":              {"https://cdn.example.test/avatars/a/b.png", bucket{}, "is not a file uploaded for this class"},
		"a file never sent":      {"https://cdn.example.test/" + key, bucket{}, "has no uploaded file yet"},
		"a file of another type": {"https://cdn.example.test/" + key, bucket{objects: map[string]*s3.Object{key: {Size: 10, ContentType: "image/png"}}}, "is not an accepted video file"},
		"a file that is too big": {
			"https://cdn.example.test/" + key,
			bucket{objects: map[string]*s3.Object{key: {Size: 101 * bytesPerMB, ContentType: "video/mp4"}}},
			"is not an accepted video file",
		},
		"a storage failure": {"https://cdn.example.test/" + key, bucket{failure: apperror.ErrStorageFailed}, "STORAGE_ERROR"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			u := &ClassroomUseCase{storage: tt.bucket, cfg: &env.Env{VideoMaxMB: 100}}

			object, err := u.checkUploadedVideo(context.Background(), classID, tt.url)

			if tt.wantErr == "" {
				require.NoError(t, err)

				if strings.HasPrefix(tt.url, "https://cdn.example.test/") {
					require.Equal(t, good, object)
				} else {
					require.Nil(t, object, "a link to another site has no stored file")
				}

				return
			}

			var appErr *apperror.Error

			require.ErrorAs(t, err, &appErr)
			require.Contains(t, fmt.Sprint(appErr.Code, appErr.Details), tt.wantErr)
		})
	}
}

func TestVideoKeyPrefixBelongsToTheClass(t *testing.T) {
	id := uuid.MustParse("3f2a9b1c-6d4e-4f70-8a12-9c0d1e2f3a4b")

	require.Equal(t, "videos/3f2a9b1c-6d4e-4f70-8a12-9c0d1e2f3a4b/", videoKeyPrefix(id))
	require.Equal(t, "pending/videos/3f2a9b1c-6d4e-4f70-8a12-9c0d1e2f3a4b/", pendingVideoKeyPrefix(id))
}

func TestApplyVideoChanges(t *testing.T) {
	text := func(value string) *string { return &value }
	number := func(value int) *int { return &value }

	start := func() *entity.LearningVideo {
		return &entity.LearningVideo{
			Title: "Old title", Description: text("Old description"), ThumbnailURL: text("https://img.example.com/a.png"),
			DurationSeconds: number(60),
		}
	}

	t.Run("a missing field stays", func(t *testing.T) {
		video := start()

		require.NoError(t, applyVideoChanges(video, dto.UpdateVideoRequest{Title: text("  New title ")}))
		require.Equal(t, "New title", video.Title)
		require.Equal(t, "Old description", *video.Description)
		require.Equal(t, "https://img.example.com/a.png", *video.ThumbnailURL)
		require.Equal(t, 60, *video.DurationSeconds)
	})

	t.Run("an empty text clears and zero clears the duration", func(t *testing.T) {
		video := start()

		require.NoError(t, applyVideoChanges(video, dto.UpdateVideoRequest{
			Description: text(" "), ThumbnailURL: text(""), DurationSeconds: number(0),
		}))
		require.Nil(t, video.Description)
		require.Nil(t, video.ThumbnailURL)
		require.Nil(t, video.DurationSeconds)
		require.Equal(t, "Old title", video.Title)
	})

	t.Run("new values replace the old ones", func(t *testing.T) {
		video := start()

		require.NoError(t, applyVideoChanges(video, dto.UpdateVideoRequest{
			Description: text("Newer"), ThumbnailURL: text("https://img.example.com/b.png"), DurationSeconds: number(90),
		}))
		require.Equal(t, "Newer", *video.Description)
		require.Equal(t, "https://img.example.com/b.png", *video.ThumbnailURL)
		require.Equal(t, 90, *video.DurationSeconds)
	})

	t.Run("bad values are refused and leave the video as it was", func(t *testing.T) {
		video := start()

		err := applyVideoChanges(video, dto.UpdateVideoRequest{Title: text("  "), ThumbnailURL: text("http://img.example.com/a.png")})

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "cannot be empty", appErr.Details["title"])
		require.Equal(t, "must be an https address", appErr.Details["thumbnailUrl"])
		require.Equal(t, "Old title", video.Title)
		require.Equal(t, "https://img.example.com/a.png", *video.ThumbnailURL)
	})
}

// countingRepo answers the one question the file cleanup asks
type countingRepo struct {
	repository.ClassroomDBItf

	remaining int
	failure   error
}

func (r countingRepo) CountVideosByURL(context.Context, string) (int, error) {
	return r.remaining, r.failure
}

func TestUnusedFileKey(t *testing.T) {
	classID := uuid.New()
	key := videoKeyPrefix(classID) + "f.mp4"
	ours := "https://cdn.example.test/" + key

	tests := map[string]struct {
		url  string
		repo countingRepo
		want string
	}{
		"the last video of an uploaded file":   {ours, countingRepo{}, key},
		"another video uses the same file":     {ours, countingRepo{remaining: 1}, ""},
		"a link to another site":               {"https://videos.example.com/a.mp4", countingRepo{}, ""},
		"the count fails":                      {ours, countingRepo{failure: context.DeadlineExceeded}, ""},
		"an address in the folder of another":  {"https://cdn.example.test/" + videoKeyPrefix(uuid.New()) + "f.mp4", countingRepo{}, ""},
		"an address outside the video folders": {"https://cdn.example.test/avatars/" + classID.String() + "/me.png", countingRepo{}, ""},
		"an address that is still staged":      {"https://cdn.example.test/" + pendingVideoKeyPrefix(classID) + "f.mp4", countingRepo{}, ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			u := &ClassroomUseCase{storage: bucket{}}

			require.Equal(t, tt.want, u.unusedFileKey(context.Background(), tt.repo, classID, tt.url))
		})
	}
}

// forumRepo holds one post and answers the membership questions from memory
type forumRepo struct {
	repository.ClassroomDBItf

	teacher *uuid.UUID
	parent  bool
	post    *repository.PostRow
	gone    bool
}

func (r *forumRepo) TeacherOfClass(context.Context, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	return r.teacher, nil
}

func (r *forumRepo) ParentInClass(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return r.parent, nil
}

func (r *forumRepo) FindPost(context.Context, uuid.UUID) (*repository.PostRow, error) {
	return r.post, nil
}

func (r *forumRepo) UpdatePost(_ context.Context, _ uuid.UUID, change func(post *entity.ForumPost)) (bool, error) {
	if r.gone {
		return false, nil
	}

	change(&r.post.ForumPost)

	return true, nil
}

func (r *forumRepo) DeletePost(context.Context, uuid.UUID) (bool, error) {
	if r.gone {
		return false, nil
	}

	r.gone = true

	return true, nil
}

func TestForumEditAndDeleteRules(t *testing.T) {
	classID, threadID, replyID := uuid.New(), uuid.New(), uuid.New()
	author, stranger, guruID := uuid.New(), uuid.New(), uuid.New()
	title := "Title"

	newRepo := func() *forumRepo {
		return &forumRepo{
			parent: true,
			post: &repository.PostRow{
				ForumPost: entity.ForumPost{ID: threadID, ClassID: classID, AuthorUserID: author, Title: &title, Body: "Text"},
			},
		}
	}

	reply := func(r *forumRepo) *forumRepo {
		r.post = &repository.PostRow{
			ForumPost: entity.ForumPost{ID: replyID, ClassID: classID, ParentID: &threadID, AuthorUserID: author, Body: "Answer"},
		}

		return r
	}

	text := func(value string) *string { return &value }

	editTime := time.Date(2026, 10, 9, 12, 0, 0, 123456789, time.UTC)
	clock := func() time.Time { return editTime }

	t.Run("the author changes a thread", func(t *testing.T) {
		u := &ClassroomUseCase{repo: newRepo(), now: clock}

		got, err := u.UpdateThread(context.Background(), author, constants.RoleWali, classID, threadID,
			dto.UpdateThreadRequest{Title: text(" New title "), Body: text("New text")})

		require.NoError(t, err)
		require.Equal(t, "New title", got.Title)
		require.Equal(t, "New text", got.Body)
	})

	t.Run("a missing field of a thread stays", func(t *testing.T) {
		u := &ClassroomUseCase{repo: newRepo(), now: clock}

		got, err := u.UpdateThread(context.Background(), author, constants.RoleWali, classID, threadID, dto.UpdateThreadRequest{Body: text("Only text")})

		require.NoError(t, err)
		require.Equal(t, "Title", got.Title)
		require.Equal(t, "Only text", got.Body)
	})

	t.Run("an empty thread change is refused", func(t *testing.T) {
		u := &ClassroomUseCase{repo: newRepo(), now: clock}

		for _, req := range []dto.UpdateThreadRequest{{}, {Title: text("  ")}, {Body: text("")}} {
			_, err := u.UpdateThread(context.Background(), author, constants.RoleWali, classID, threadID, req)

			var appErr *apperror.Error

			require.ErrorAs(t, err, &appErr)
			require.Equal(t, "VALIDATION_ERROR", appErr.Code)
		}
	})

	t.Run("only the author edits", func(t *testing.T) {
		repo := newRepo()
		repo.teacher = &guruID
		u := &ClassroomUseCase{repo: repo, now: clock}

		_, err := u.UpdateThread(context.Background(), stranger, constants.RoleGuru, classID, threadID, dto.UpdateThreadRequest{Body: text("x")})
		require.ErrorIs(t, err, apperror.ErrForbidden, "a teacher may delete but not edit the words of someone else")

		repliesRepo := reply(newRepo())
		repliesRepo.teacher = &guruID

		_, err = (&ClassroomUseCase{repo: repliesRepo, now: clock}).UpdateReply(
			context.Background(), stranger, constants.RoleGuru, classID, threadID, replyID, dto.UpdateReplyRequest{Body: "x"})
		require.ErrorIs(t, err, apperror.ErrForbidden)
	})

	t.Run("the author changes a reply", func(t *testing.T) {
		u := &ClassroomUseCase{repo: reply(newRepo()), now: clock}

		got, err := u.UpdateReply(context.Background(), author, constants.RoleWali, classID, threadID, replyID, dto.UpdateReplyRequest{Body: " Better "})

		require.NoError(t, err)
		require.Equal(t, "Better", got.Body)

		_, err = u.UpdateReply(context.Background(), author, constants.RoleWali, classID, threadID, replyID, dto.UpdateReplyRequest{Body: "  "})
		require.Error(t, err)
	})

	t.Run("an edit that changes the words marks the post as edited", func(t *testing.T) {
		u := &ClassroomUseCase{repo: newRepo(), now: clock}

		before, err := u.UpdateThread(context.Background(), author, constants.RoleWali, classID, threadID, dto.UpdateThreadRequest{Body: text("Text")})
		require.NoError(t, err)
		require.Nil(t, before.EditedAt, "the same words are not an edit")

		changed, err := u.UpdateThread(context.Background(), author, constants.RoleWali, classID, threadID, dto.UpdateThreadRequest{Title: text("Other")})
		require.NoError(t, err)
		require.NotNil(t, changed.EditedAt)
		require.Equal(t, editTime.Truncate(time.Microsecond), *changed.EditedAt)

		replyUseCase := &ClassroomUseCase{repo: reply(newRepo()), now: clock}

		same, err := replyUseCase.UpdateReply(context.Background(), author, constants.RoleWali, classID, threadID, replyID, dto.UpdateReplyRequest{Body: " Answer "})
		require.NoError(t, err)
		require.Nil(t, same.EditedAt)

		edited, err := replyUseCase.UpdateReply(context.Background(), author, constants.RoleWali, classID, threadID, replyID, dto.UpdateReplyRequest{Body: "Better"})
		require.NoError(t, err)
		require.NotNil(t, edited.EditedAt)
	})

	t.Run("a save without changes keeps the old edit time", func(t *testing.T) {
		earlier := editTime.Add(-time.Hour)
		repo := newRepo()
		repo.post.EditedAt = &earlier

		got, err := (&ClassroomUseCase{repo: repo, now: clock}).UpdateThread(
			context.Background(), author, constants.RoleWali, classID, threadID, dto.UpdateThreadRequest{Body: text("Text")})

		require.NoError(t, err)
		require.Equal(t, &earlier, got.EditedAt)
	})

	t.Run("a post removed while it is saved is not found", func(t *testing.T) {
		repo := newRepo()
		repo.gone = true

		_, err := (&ClassroomUseCase{repo: repo, now: clock}).UpdateThread(
			context.Background(), author, constants.RoleWali, classID, threadID, dto.UpdateThreadRequest{Body: text("x")})

		require.ErrorIs(t, err, apperror.ErrForumPostNotFound)
	})

	t.Run("a post of another place is not found", func(t *testing.T) {
		u := &ClassroomUseCase{repo: newRepo(), now: clock}

		_, err := u.UpdateThread(context.Background(), author, constants.RoleWali, uuid.New(), threadID, dto.UpdateThreadRequest{Body: text("x")})
		require.ErrorIs(t, err, apperror.ErrForumPostNotFound, "the post belongs to another class")

		require.ErrorIs(t, u.DeleteReply(context.Background(), author, constants.RoleWali, classID, threadID, threadID), apperror.ErrForumPostNotFound,
			"a thread is not a reply")

		replies := &ClassroomUseCase{repo: reply(newRepo()), now: clock}

		require.ErrorIs(t, replies.DeleteReply(context.Background(), author, constants.RoleWali, classID, uuid.New(), replyID), apperror.ErrForumPostNotFound,
			"the reply belongs to another thread")
		require.ErrorIs(t, replies.DeleteThread(context.Background(), author, constants.RoleWali, classID, replyID), apperror.ErrForumPostNotFound,
			"a reply is not a thread")

		missing := newRepo()
		missing.post = nil

		require.ErrorIs(t, (&ClassroomUseCase{repo: missing}).DeleteThread(context.Background(), author, constants.RoleWali, classID, threadID),
			apperror.ErrForumPostNotFound)
	})

	t.Run("the author or a teacher deletes", func(t *testing.T) {
		own := newRepo()
		require.NoError(t, (&ClassroomUseCase{repo: own}).DeleteThread(context.Background(), author, constants.RoleWali, classID, threadID))
		require.True(t, own.gone)

		moderated := reply(newRepo())
		moderated.teacher = &guruID
		require.NoError(t, (&ClassroomUseCase{repo: moderated}).DeleteReply(context.Background(), stranger, constants.RoleGuru, classID, threadID, replyID))
		require.True(t, moderated.gone)
	})

	t.Run("another parent cannot delete", func(t *testing.T) {
		repo := newRepo()

		require.ErrorIs(t, (&ClassroomUseCase{repo: repo, now: clock}).DeleteThread(context.Background(), stranger, constants.RoleWali, classID, threadID),
			apperror.ErrForbidden)
		require.False(t, repo.gone)
	})

	t.Run("someone outside the class cannot touch the forum", func(t *testing.T) {
		repo := newRepo()
		repo.parent = false

		require.ErrorIs(t, (&ClassroomUseCase{repo: repo, now: clock}).DeleteThread(context.Background(), author, constants.RoleWali, classID, threadID),
			apperror.ErrForbidden)
	})

	t.Run("a post removed by another request is not found", func(t *testing.T) {
		repo := newRepo()
		repo.gone = true

		require.ErrorIs(t, (&ClassroomUseCase{repo: repo, now: clock}).DeleteThread(context.Background(), author, constants.RoleWali, classID, threadID),
			apperror.ErrForumPostNotFound)
	})
}

// lockRepo records what happens in which order, so the tests can see what runs under the file lock
type lockRepo struct {
	repository.ClassroomDBItf

	events    *[]string
	teacher   uuid.UUID
	video     *entity.LearningVideo
	remaining int
	createErr error
}

func (r lockRepo) TeacherOfClass(context.Context, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	return &r.teacher, nil
}

func (r lockRepo) FindVideo(context.Context, uuid.UUID, uuid.UUID) (*entity.LearningVideo, error) {
	return r.video, nil
}

// UpdateVideo edits a copy of the video the way the locked row is edited, and keeps it only when the change succeeds
func (r lockRepo) UpdateVideo(_ context.Context, _ uuid.UUID, _ uuid.UUID, change func(video *entity.LearningVideo) error) (*entity.LearningVideo, error) {
	if r.video == nil {
		return nil, nil
	}

	edited := *r.video

	err := change(&edited)
	if err != nil {
		return nil, err
	}

	*r.events = append(*r.events, "update")

	return &edited, nil
}

func (r lockRepo) CreateVideo(context.Context, *entity.LearningVideo) error {
	*r.events = append(*r.events, "create")

	return r.createErr
}

func (r lockRepo) DeleteVideo(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	*r.events = append(*r.events, "delete")

	return true, nil
}

func (r lockRepo) CountVideosByURL(context.Context, string) (int, error) {
	*r.events = append(*r.events, "count")

	return r.remaining, nil
}

func (r lockRepo) WithVideoFileLock(_ context.Context, _ string, fn func(repo repository.ClassroomDBItf) error) error {
	*r.events = append(*r.events, "lock")
	err := fn(r)
	*r.events = append(*r.events, "unlock")

	return err
}

// orderedBucket adds the copy and the deletion of a file to the events of the repository
type orderedBucket struct {
	bucket

	events  *[]string
	deleted *[]string
	copyErr error
}

func (b orderedBucket) Copy(_ context.Context, source string, key string, contentType string) error {
	*b.events = append(*b.events, "copy "+source+" to "+key+" as "+contentType)

	return b.copyErr
}

func (b orderedBucket) Delete(_ context.Context, key string) error {
	*b.events = append(*b.events, "discard")

	if b.deleted != nil {
		*b.deleted = append(*b.deleted, key)
	}

	return nil
}

func TestVideoFileLock(t *testing.T) {
	classID, videoID := uuid.New(), uuid.New()
	userID := uuid.New()
	key := videoKeyPrefix(classID) + uuid.NewString() + ".mp4"
	uploadedURL := "https://cdn.example.test/" + key

	build := func(events *[]string, remaining int, files map[string]*s3.Object) *ClassroomUseCase {
		repo := lockRepo{
			events: events, teacher: uuid.New(), remaining: remaining,
			video: &entity.LearningVideo{ID: videoID, ClassID: classID, VideoURL: uploadedURL},
		}

		return &ClassroomUseCase{
			repo:    repo,
			storage: orderedBucket{bucket: bucket{objects: files}, events: events},
			cfg:     &env.Env{VideoMaxMB: 100},
			now:     time.Now,
		}
	}

	present := map[string]*s3.Object{key: {Size: 10, ContentType: "video/mp4"}}

	t.Run("adding the address of a file that a video uses checks and stores it under the lock", func(t *testing.T) {
		var events []string

		_, err := build(&events, 1, present).AddVideo(context.Background(), userID, classID, dto.AddVideoRequest{Title: "Lesson", VideoURL: uploadedURL})

		require.NoError(t, err)
		require.Equal(t, []string{"lock", "count", "create", "unlock"}, events)
	})

	t.Run("the address of a file that no video uses cannot be claimed", func(t *testing.T) {
		var events []string

		_, err := build(&events, 0, present).AddVideo(context.Background(), userID, classID, dto.AddVideoRequest{Title: "Lesson", VideoURL: uploadedURL})

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "VALIDATION_ERROR", appErr.Code)
		require.Contains(t, appErr.Details["videoUrl"], "already uses")
		require.Equal(t, []string{"lock", "count", "unlock"}, events)
	})

	t.Run("adding a file that is gone stores nothing", func(t *testing.T) {
		var events []string

		_, err := build(&events, 0, nil).AddVideo(context.Background(), userID, classID, dto.AddVideoRequest{Title: "Lesson", VideoURL: uploadedURL})

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "VALIDATION_ERROR", appErr.Code)
		require.Equal(t, []string{"lock", "unlock"}, events)
	})

	t.Run("a link to another site needs no lock", func(t *testing.T) {
		var events []string

		_, err := build(&events, 0, nil).AddVideo(context.Background(), userID, classID,
			dto.AddVideoRequest{Title: "Lesson", VideoURL: "https://videos.example.com/a.mp4"})

		require.NoError(t, err)
		require.Equal(t, []string{"create"}, events)
	})

	t.Run("the file is removed only after the video is gone and the lock is released", func(t *testing.T) {
		var events []string

		require.NoError(t, build(&events, 0, present).DeleteVideo(context.Background(), userID, classID, videoID))
		require.Equal(t, []string{"lock", "delete", "count", "unlock", "discard"}, events)
	})

	t.Run("a file is kept when the delete fails", func(t *testing.T) {
		var events []string

		u := build(&events, 0, present)
		u.repo = failingDeleteRepo{lockRepo: u.repo.(lockRepo)}

		require.Error(t, u.DeleteVideo(context.Background(), userID, classID, videoID))
		require.NotContains(t, events, "discard")
	})

	t.Run("an edit is applied to the video and reported", func(t *testing.T) {
		var events []string

		title := "New title"
		got, err := build(&events, 0, present).UpdateVideo(context.Background(), userID, classID, videoID, dto.UpdateVideoRequest{Title: &title})

		require.NoError(t, err)
		require.Equal(t, "New title", got.Title)
		require.Equal(t, []string{"update"}, events)
	})

	t.Run("an invalid edit changes nothing", func(t *testing.T) {
		var events []string

		blank := "  "
		_, err := build(&events, 0, present).UpdateVideo(context.Background(), userID, classID, videoID, dto.UpdateVideoRequest{Title: &blank})

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "VALIDATION_ERROR", appErr.Code)
		require.Empty(t, events)
	})

	t.Run("an edit of a video that is gone is not found", func(t *testing.T) {
		var events []string

		u := build(&events, 0, present)
		u.repo = lockRepo{events: &events, teacher: uuid.New()}

		title := "New title"
		_, err := u.UpdateVideo(context.Background(), userID, classID, videoID, dto.UpdateVideoRequest{Title: &title})

		require.ErrorIs(t, err, apperror.ErrVideoNotFound)
	})

	t.Run("a file that another video uses stays", func(t *testing.T) {
		var events []string

		require.NoError(t, build(&events, 1, present).DeleteVideo(context.Background(), userID, classID, videoID))
		require.Equal(t, []string{"lock", "delete", "count", "unlock"}, events)
	})
}

// failingDeleteRepo cannot delete the video, as if the transaction had failed
type failingDeleteRepo struct {
	lockRepo
}

func (r failingDeleteRepo) DeleteVideo(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, context.DeadlineExceeded
}

// WithVideoFileLock hands this repository to fn and not the embedded one, so the failing delete is the one that runs
func (r failingDeleteRepo) WithVideoFileLock(_ context.Context, _ string, fn func(repo repository.ClassroomDBItf) error) error {
	return fn(r)
}

func TestAddStagedVideo(t *testing.T) {
	classID, userID := uuid.New(), uuid.New()
	name := uuid.NewString() + ".mp4"
	stagedKey := pendingVideoKeyPrefix(classID) + name
	permanentKey := videoKeyPrefix(classID) + name
	stagedURL := "https://cdn.example.test/" + stagedKey
	permanentURL := "https://cdn.example.test/" + permanentKey

	files := map[string]*s3.Object{stagedKey: {Size: 10, ContentType: "video/mp4"}}

	build := func(events *[]string, deleted *[]string, createErr error, copyErr error, existing int) *ClassroomUseCase {
		return &ClassroomUseCase{
			repo:    lockRepo{events: events, teacher: uuid.New(), createErr: createErr, remaining: existing},
			storage: orderedBucket{bucket: bucket{objects: files}, events: events, deleted: deleted, copyErr: copyErr},
			cfg:     &env.Env{VideoMaxMB: 100},
			now:     time.Now,
		}
	}

	req := dto.AddVideoRequest{Title: "Lesson", VideoURL: stagedURL}

	t.Run("the file is copied to its permanent key and the staged file goes", func(t *testing.T) {
		var events, deleted []string

		video, err := build(&events, &deleted, nil, nil, 0).AddVideo(context.Background(), userID, classID, req)

		require.NoError(t, err)
		require.Equal(t, permanentURL, video.VideoURL)
		require.Equal(t, []string{
			"lock", "count", "copy " + stagedKey + " to " + permanentKey + " as video/mp4", "create", "unlock", "discard",
		}, events)
		require.Equal(t, []string{stagedKey}, deleted)
	})

	t.Run("a failed copy stores nothing", func(t *testing.T) {
		var events, deleted []string

		_, err := build(&events, &deleted, nil, apperror.ErrStorageFailed, 0).AddVideo(context.Background(), userID, classID, req)

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "STORAGE_ERROR", appErr.Code)
		require.Equal(t, []string{"lock", "count", "copy " + stagedKey + " to " + permanentKey + " as video/mp4", "unlock"}, events)
		require.Empty(t, deleted)
	})

	t.Run("a video that cannot be stored removes the copy and keeps the staged file", func(t *testing.T) {
		var events, deleted []string

		_, err := build(&events, &deleted, errors.New("insert failed"), nil, 0).AddVideo(context.Background(), userID, classID, req)

		require.Error(t, err)
		require.Equal(t, []string{
			"lock", "count", "copy " + stagedKey + " to " + permanentKey + " as video/mp4", "create", "discard", "unlock",
		}, events)
		require.Equal(t, []string{permanentKey}, deleted, "the staged file expires by itself")
	})

	t.Run("a staged file that another request already added is refused", func(t *testing.T) {
		var events, deleted []string

		_, err := build(&events, &deleted, nil, nil, 1).AddVideo(context.Background(), userID, classID, req)

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "VALIDATION_ERROR", appErr.Code)
		require.Equal(t, "has already been added", appErr.Details["videoUrl"])
		require.Equal(t, []string{"lock", "count", "unlock"}, events, "nothing is copied or stored")
		require.Empty(t, deleted)
	})

	t.Run("a staged file of another class is refused before anything is copied", func(t *testing.T) {
		var events, deleted []string

		_, err := build(&events, &deleted, nil, nil, 0).AddVideo(context.Background(), userID, uuid.New(), req)

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "VALIDATION_ERROR", appErr.Code)
		require.NotContains(t, strings.Join(events, ","), "copy")
	})
}

func TestApplyPostChangesWorksOnTheCurrentRow(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	u := &ClassroomUseCase{now: func() time.Time { return now }}

	text := func(value string) *string { return &value }

	current := func() *entity.ForumPost {
		return &entity.ForumPost{Title: text("Title"), Body: "Newer words", EditedAt: &earlier}
	}

	t.Run("words that match the current row change nothing", func(t *testing.T) {
		post := current()

		u.applyPostChanges(post, text("Title"), text("Newer words"))

		require.Equal(t, "Newer words", post.Body)
		require.Equal(t, &earlier, post.EditedAt, "a delayed save cannot clear or move the mark of a newer edit")
	})

	t.Run("a missing field stays", func(t *testing.T) {
		post := current()

		u.applyPostChanges(post, nil, text("Changed"))

		require.Equal(t, "Title", *post.Title)
		require.Equal(t, "Changed", post.Body)
		require.Equal(t, &now, post.EditedAt)
	})

	t.Run("a new title marks the post", func(t *testing.T) {
		post := current()

		u.applyPostChanges(post, text("Other"), nil)

		require.Equal(t, "Other", *post.Title)
		require.Equal(t, "Newer words", post.Body)
		require.Equal(t, &now, post.EditedAt)
	})

	t.Run("a reply keeps its empty title", func(t *testing.T) {
		post := &entity.ForumPost{Body: "Answer"}

		u.applyPostChanges(post, nil, text("Better"))

		require.Nil(t, post.Title)
		require.Equal(t, &now, post.EditedAt)
	})
}
