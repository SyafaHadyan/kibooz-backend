package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/constants"
)

func TestParseThreadFilter(t *testing.T) {
	app := fiber.New()

	var (
		gotSubject *constants.SubjectCode
		gotGeneral bool
		gotErr     error
	)

	app.Get("/filter", func(c fiber.Ctx) error {
		gotSubject, gotGeneral, gotErr = parseThreadFilter(c)
		if gotErr != nil {
			return c.SendStatus(http.StatusBadRequest)
		}

		return c.SendStatus(http.StatusOK)
	})

	callFilter := func(query string) int {
		req := httptest.NewRequest(http.MethodGet, "/filter"+query, nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())

		return resp.StatusCode
	}

	require.Equal(t, http.StatusOK, callFilter(""))
	require.Nil(t, gotSubject)
	require.False(t, gotGeneral)
	require.NoError(t, gotErr)

	require.Equal(t, http.StatusOK, callFilter("?subjectCode=SENI-A1"))
	require.NotNil(t, gotSubject)
	require.Equal(t, constants.SubjectSeni, *gotSubject)
	require.False(t, gotGeneral)

	require.Equal(t, http.StatusOK, callFilter("?generalOnly=true"))
	require.Nil(t, gotSubject)
	require.True(t, gotGeneral)

	require.Equal(t, http.StatusBadRequest, callFilter("?subjectCode=INVALID"))
	require.Error(t, gotErr)

	require.Equal(t, http.StatusBadRequest, callFilter("?generalOnly=abc"))
	require.Error(t, gotErr)

	require.Equal(t, http.StatusBadRequest, callFilter("?subjectCode=SENI-A1&generalOnly=true"))
	require.Error(t, gotErr)
}
