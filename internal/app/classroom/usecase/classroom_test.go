package usecase

import (
	"context"
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

func (b bucket) Stat(_ context.Context, key string) (*s3.Object, error) {
	return b.objects[key], b.failure
}

func TestCheckUploadedVideo(t *testing.T) {
	classID := uuid.New()
	key := videoKeyPrefix(classID) + uuid.NewString() + ".mp4"
	other := videoKeyPrefix(uuid.New()) + uuid.NewString() + ".mp4"

	good := &s3.Object{Size: 5 * bytesPerMB, ContentType: "video/mp4"}

	tests := map[string]struct {
		url     string
		bucket  bucket
		wantErr string
	}{
		"a link to another site":  {"https://videos.example.com/a.mp4", bucket{}, ""},
		"a finished upload":       {"https://cdn.example.test/" + key, bucket{objects: map[string]*s3.Object{key: good}}, ""},
		"a file of another class": {"https://cdn.example.test/" + other, bucket{objects: map[string]*s3.Object{other: good}}, "is not a file uploaded for this class"},
		"an avatar":               {"https://cdn.example.test/avatars/a/b.png", bucket{}, "is not a file uploaded for this class"},
		"a file never sent":       {"https://cdn.example.test/" + key, bucket{}, "has no uploaded file yet"},
		"a file of another type":  {"https://cdn.example.test/" + key, bucket{objects: map[string]*s3.Object{key: {Size: 10, ContentType: "image/png"}}}, "is not an accepted video file"},
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

			err := u.checkUploadedVideo(context.Background(), classID, tt.url)

			if tt.wantErr == "" {
				require.NoError(t, err)
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

// removalBucket records which objects were deleted
type removalBucket struct {
	bucket

	deleted *[]string
}

func (b removalBucket) Delete(_ context.Context, key string) error {
	*b.deleted = append(*b.deleted, key)

	return nil
}

func TestRemoveUploadedFile(t *testing.T) {
	ours := "https://cdn.example.test/videos/c/f.mp4"

	tests := map[string]struct {
		url  string
		repo countingRepo
		want []string
	}{
		"the last video of an uploaded file": {ours, countingRepo{}, []string{"videos/c/f.mp4"}},
		"another video uses the same file":   {ours, countingRepo{remaining: 1}, nil},
		"a link to another site":             {"https://videos.example.com/a.mp4", countingRepo{}, nil},
		"the count fails":                    {ours, countingRepo{failure: context.DeadlineExceeded}, nil},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var deleted []string

			u := &ClassroomUseCase{storage: removalBucket{deleted: &deleted}}

			u.removeUploadedFile(context.Background(), tt.repo, tt.url)

			require.Equal(t, tt.want, deleted)
		})
	}
}

// lockRepo records what happens in which order, so the tests can see what runs under the file lock
type lockRepo struct {
	repository.ClassroomDBItf

	events    *[]string
	teacher   uuid.UUID
	video     *entity.LearningVideo
	remaining int
}

func (r lockRepo) TeacherOfClass(context.Context, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	return &r.teacher, nil
}

func (r lockRepo) FindVideo(context.Context, uuid.UUID, uuid.UUID) (*entity.LearningVideo, error) {
	return r.video, nil
}

func (r lockRepo) CreateVideo(context.Context, *entity.LearningVideo) error {
	*r.events = append(*r.events, "create")

	return nil
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

// orderedBucket adds the deletion of a file to the events of the repository
type orderedBucket struct {
	bucket

	events *[]string
}

func (b orderedBucket) Delete(context.Context, string) error {
	*b.events = append(*b.events, "discard")

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

	t.Run("adding an uploaded file checks and stores it under the lock", func(t *testing.T) {
		var events []string

		_, err := build(&events, 0, present).AddVideo(context.Background(), userID, classID, dto.AddVideoRequest{Title: "Lesson", VideoURL: uploadedURL})

		require.NoError(t, err)
		require.Equal(t, []string{"lock", "create", "unlock"}, events)
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

	t.Run("the file is removed while the lock is held", func(t *testing.T) {
		var events []string

		require.NoError(t, build(&events, 0, present).DeleteVideo(context.Background(), userID, classID, videoID))
		require.Equal(t, []string{"lock", "delete", "count", "discard", "unlock"}, events)
	})

	t.Run("a file that another video uses stays", func(t *testing.T) {
		var events []string

		require.NoError(t, build(&events, 1, present).DeleteVideo(context.Background(), userID, classID, videoID))
		require.Equal(t, []string{"lock", "delete", "count", "unlock"}, events)
	})
}
