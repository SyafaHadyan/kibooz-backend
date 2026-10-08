package usecase

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
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
