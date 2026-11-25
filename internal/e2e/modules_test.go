package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLearningModules(t *testing.T) {
	guru := registerGuru(t, "Sakura Modul")
	classID, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Sakura Modul Child")

	otherGuru := registerGuru(t, "Kamboja Modul")
	otherClassID, otherCode := classOf(t, otherGuru)
	otherWali, _ := registerWali(t, otherCode, "Kamboja Modul Child")

	modulesPath := fmt.Sprintf("/api/v1/classes/%s/modules", classID)

	t.Run("a new class has no modules", func(t *testing.T) {
		for name, token := range map[string]string{"teacher": guru.Token, "parent": wali.Token} {
			res := call(t, http.MethodGet, modulesPath, token, nil)
			require.Equal(t, http.StatusOK, res.Status, "%s body %v", name, res.Body)
			require.Empty(t, listOf(t, res, "modules"))
			require.EqualValues(t, 1, res.data("page"))
			require.EqualValues(t, 20, res.data("limit"))
			require.EqualValues(t, 0, res.data("total"))
		}
	})

	t.Run("only members read and only teachers write modules", func(t *testing.T) {
		body := map[string]any{"meetingNumber": 1, "title": "Pertemuan 1"}

		call(t, http.MethodPost, modulesPath, wali.Token, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, modulesPath, otherGuru.Token, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, modulesPath, otherGuru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, modulesPath, otherWali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, modulesPath, "", body).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	var firstID, secondID string

	t.Run("teachers create modules ordered by meeting number", func(t *testing.T) {
		second := call(t, http.MethodPost, modulesPath, guru.Token, map[string]any{
			"meetingNumber": 2,
			"title":         "  Pertemuan 2: Menggambar Ekspresi  ",
			"description":   "Membawa pensil warna",
			"status":        "ongoing",
			"scheduledAt":   "2026-11-28T08:00:00Z",
		})
		require.Equal(t, http.StatusCreated, second.Status, "body %v", second.Body)
		require.Equal(t, "Learning module added", second.Body["message"])
		require.Equal(t, "Pertemuan 2: Menggambar Ekspresi", second.data("title"))
		require.Equal(t, "Membawa pensil warna", second.data("description"))
		require.Equal(t, "ongoing", second.data("status"))
		secondID = second.data("id").(string)

		first := call(t, http.MethodPost, modulesPath, guru.Token, map[string]any{
			"meetingNumber": 1,
			"title":         "Pertemuan 1: Mengenal Emosi",
		})
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)
		require.Equal(t, "upcoming", first.data("status"))
		require.Nil(t, first.data("description"))
		require.Nil(t, first.data("scheduledAt"))
		firstID = first.data("id").(string)

		listed := call(t, http.MethodGet, modulesPath, wali.Token, nil)
		require.Equal(t, http.StatusOK, listed.Status, "body %v", listed.Body)
		require.EqualValues(t, 2, listed.data("total"))
		require.Equal(t, []string{"Pertemuan 1: Mengenal Emosi", "Pertemuan 2: Menggambar Ekspresi"},
			titles(t, listOf(t, listed, "modules"), "title"))
	})

	t.Run("bad module input is refused", func(t *testing.T) {
		for _, body := range []map[string]any{
			{},
			{"meetingNumber": 0, "title": "x"},
			{"meetingNumber": 101, "title": "x"},
			{"meetingNumber": 1, "title": "   "},
			{"meetingNumber": 1, "title": strings.Repeat("a", 151)},
			{"meetingNumber": 1, "title": "x", "status": "unknown"},
		} {
			call(t, http.MethodPost, modulesPath, guru.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}
	})

	t.Run("teachers update and delete modules while outsider cannot", func(t *testing.T) {
		updated := call(t, http.MethodPatch, modulesPath+"/"+firstID, guru.Token, map[string]any{
			"title":       " Pertemuan 1 Revisi ",
			"description": "",
			"status":      "completed",
		})
		require.Equal(t, http.StatusOK, updated.Status, "body %v", updated.Body)
		require.Equal(t, "Learning module updated", updated.Body["message"])
		require.Equal(t, "Pertemuan 1 Revisi", updated.data("title"))
		require.Nil(t, updated.data("description"))
		require.Equal(t, "completed", updated.data("status"))

		call(t, http.MethodPatch, modulesPath+"/"+firstID, wali.Token, map[string]any{"title": "No"}).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodDelete, modulesPath+"/"+firstID, otherGuru.Token, nil).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		theirs := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/modules", otherClassID), otherGuru.Token,
			map[string]any{"meetingNumber": 1, "title": "Other"})
		require.Equal(t, http.StatusCreated, theirs.Status)

		for _, missingID := range []string{uuid.NewString(), theirs.data("id").(string)} {
			call(t, http.MethodPatch, modulesPath+"/"+missingID, guru.Token, map[string]any{"title": "X"}).
				requireError(t, http.StatusNotFound, "MODULE_NOT_FOUND")
			call(t, http.MethodDelete, modulesPath+"/"+missingID, guru.Token, nil).
				requireError(t, http.StatusNotFound, "MODULE_NOT_FOUND")
		}

		del := call(t, http.MethodDelete, modulesPath+"/"+secondID, guru.Token, nil)
		require.Equal(t, http.StatusOK, del.Status, "body %v", del.Body)
		require.Equal(t, "Learning module deleted", del.Body["message"])

		listed := call(t, http.MethodGet, modulesPath, guru.Token, nil)
		require.EqualValues(t, 1, listed.data("total"))
	})

	t.Run("archived class refuses module mutations", func(t *testing.T) {
		arch := call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/archive", guru.Token, nil)
		require.Equal(t, http.StatusOK, arch.Status)

		call(t, http.MethodPost, modulesPath, guru.Token, map[string]any{"meetingNumber": 3, "title": "No"}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPatch, modulesPath+"/"+firstID, guru.Token, map[string]any{"title": "No"}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodDelete, modulesPath+"/"+firstID, guru.Token, nil).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, modulesPath, wali.Token, nil).Status)
	})
}
