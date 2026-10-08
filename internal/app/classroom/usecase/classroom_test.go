package usecase

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
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
