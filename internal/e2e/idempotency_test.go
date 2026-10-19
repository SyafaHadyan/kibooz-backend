package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/idempotency"
)

const (
	moodLogPath   = "/api/v1/guru/mood/log"
	scanClaimPath = "/api/v1/trash/scan-claim"
)

// callKey is call with an Idempotency-Key, which also hands back the headers of the answer
func callKey(t *testing.T, method string, path string, token string, key string, body any) (result, http.Header) {
	t.Helper()

	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	keepBody(req, encoded)
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if key != "" {
		req.Header.Set(idempotency.Header, key)
	}

	res, err := app(t).Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(t, err)

	defer res.Body.Close()

	return decodeChecked(t, req, res), res.Header
}

// fingerprintOf is the fingerprint the middleware keeps for a request, so a test can plant a key as if a request had made it
func fingerprintOf(t *testing.T, method string, path string, body any) []byte {
	t.Helper()

	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	sum := sha256.New()
	sum.Write([]byte(method))
	sum.Write([]byte{0})
	sum.Write([]byte(path))
	sum.Write([]byte{0})
	sum.Write(encoded)

	return sum.Sum(nil)
}

func userIDOf(t *testing.T, email string) string {
	t.Helper()

	var id string

	require.NoError(t, testDB.Raw("SELECT id FROM users WHERE email = ?", email).Scan(&id).Error)
	require.NotEmpty(t, id)

	return id
}

func keyRows(t *testing.T, userID string, key string) int64 {
	t.Helper()

	var total int64

	require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM idempotency_keys WHERE user_id = ? AND idempotency_key = ?", userID, key).Scan(&total).Error)

	return total
}

func scanCount(t *testing.T, studentID string) int64 {
	t.Helper()

	var total int64

	require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM trash_scans WHERE student_id = ?", studentID).Scan(&total).Error)

	return total
}

func TestIdempotencyKey(t *testing.T) {
	guru := registerGuru(t, "Melati")
	classID, joinCode := classOf(t, guru)
	amelia, ameliaID := registerWali(t, joinCode, "Amelia Siti Zahra")
	guruID := userIDOf(t, guru.Email)

	t.Run("a repeat gets the first answer and writes nothing twice", func(t *testing.T) {
		key := uuid.NewString()
		body := map[string]any{"studentId": ameliaID, "moodType": "SENANG"}

		first, firstHeader := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, body)
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)
		require.Empty(t, firstHeader.Get(idempotency.ReplayedHeader))

		again, againHeader := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, body)
		require.Equal(t, http.StatusCreated, again.Status, "body %v", again.Body)
		require.Equal(t, "true", againHeader.Get(idempotency.ReplayedHeader))
		require.Equal(t, first.Body, again.Body, "the repeat has to carry the first answer")

		require.EqualValues(t, 1, moodLogCount(t, ameliaID))
	})

	t.Run("a request without a key runs every time", func(t *testing.T) {
		before := moodLogCount(t, ameliaID)
		body := map[string]any{"studentId": ameliaID, "moodType": "SEDIH"}

		for range 2 {
			res, header := callKey(t, http.MethodPost, moodLogPath, guru.Token, "", body)
			require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
			require.Empty(t, header.Get(idempotency.ReplayedHeader))
		}

		require.EqualValues(t, before+2, moodLogCount(t, ameliaID))
	})

	t.Run("the same key for another request is refused and changes nothing", func(t *testing.T) {
		key := uuid.NewString()
		before := moodLogCount(t, ameliaID)

		first, _ := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, map[string]any{"studentId": ameliaID, "moodType": "SENANG"})
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)

		other, _ := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, map[string]any{"studentId": ameliaID, "moodType": "MARAH"})
		other.requireError(t, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED")

		require.EqualValues(t, before+1, moodLogCount(t, ameliaID))
	})

	t.Run("an answer that is not a success frees the key", func(t *testing.T) {
		key := uuid.NewString()
		before := moodLogCount(t, ameliaID)

		refused, _ := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, map[string]any{"studentId": ameliaID, "moodType": "LAPAR"})
		refused.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		require.EqualValues(t, 0, keyRows(t, guruID, key), "nothing is kept for a refused request")

		// the fixed request goes through with the same key
		fixed, _ := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, map[string]any{"studentId": ameliaID, "moodType": "SENANG"})
		require.Equal(t, http.StatusCreated, fixed.Status, "body %v", fixed.Body)
		require.EqualValues(t, before+1, moodLogCount(t, ameliaID))
	})

	t.Run("a key belongs to the user who sent it", func(t *testing.T) {
		key := uuid.NewString()

		otherGuru := registerGuru(t, "Dahlia")
		_, otherCode := classOf(t, otherGuru)
		_, otherChildID := registerWali(t, otherCode, "Other Child")

		first, _ := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, map[string]any{"studentId": ameliaID, "moodType": "SENANG"})
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)

		second, header := callKey(t, http.MethodPost, moodLogPath, otherGuru.Token, key, map[string]any{"studentId": otherChildID, "moodType": "SENANG"})
		require.Equal(t, http.StatusCreated, second.Status, "body %v", second.Body)
		require.Empty(t, header.Get(idempotency.ReplayedHeader), "another user never gets the answer of the first")
		require.EqualValues(t, 1, moodLogCount(t, otherChildID))
	})

	t.Run("a key that is not 8 to 64 safe characters is refused", func(t *testing.T) {
		before := moodLogCount(t, ameliaID)
		body := map[string]any{"studentId": ameliaID, "moodType": "SENANG"}

		for _, key := range []string{"short", "has spaces in it 123", "ünïcode-key-12345", string(bytes.Repeat([]byte("a"), 65))} {
			res, _ := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, body)
			res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
			requireDetail(t, res, idempotency.Header)
		}

		require.EqualValues(t, before, moodLogCount(t, ameliaID))
	})

	t.Run("a request whose first try still runs is told to wait", func(t *testing.T) {
		key := uuid.NewString()
		body := map[string]any{"studentId": ameliaID, "moodType": "SENANG"}
		before := moodLogCount(t, ameliaID)

		require.NoError(t, testDB.Exec(
			"INSERT INTO idempotency_keys (user_id, idempotency_key, fingerprint) VALUES (?, ?, ?)",
			guruID, key, fingerprintOf(t, http.MethodPost, moodLogPath, body),
		).Error)

		res, _ := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, body)
		res.requireError(t, http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS")
		require.EqualValues(t, before, moodLogCount(t, ameliaID))
	})

	t.Run("a first try that never finished is taken over after it is abandoned", func(t *testing.T) {
		key := uuid.NewString()
		body := map[string]any{"studentId": ameliaID, "moodType": "SENANG"}
		before := moodLogCount(t, ameliaID)

		require.NoError(t, testDB.Exec(
			"INSERT INTO idempotency_keys (user_id, idempotency_key, fingerprint, created_at) VALUES (?, ?, ?, now() - interval '1 hour')",
			guruID, key, fingerprintOf(t, http.MethodPost, moodLogPath, body),
		).Error)

		res, header := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, body)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
		require.Empty(t, header.Get(idempotency.ReplayedHeader))
		require.EqualValues(t, before+1, moodLogCount(t, ameliaID))

		// and the answer of the takeover is the one that a repeat gets
		again, againHeader := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, body)
		require.Equal(t, "true", againHeader.Get(idempotency.ReplayedHeader))
		require.Equal(t, res.Body, again.Body)
	})

	t.Run("a key is forgotten once it is older than the retention", func(t *testing.T) {
		key := uuid.NewString()
		body := map[string]any{"studentId": ameliaID, "moodType": "SENANG"}
		before := moodLogCount(t, ameliaID)

		require.NoError(t, testDB.Exec(
			"INSERT INTO idempotency_keys (user_id, idempotency_key, fingerprint, status_code, response, created_at) "+
				"VALUES (?, ?, ?, 201, ?, now() - interval '8 days')",
			guruID, key, fingerprintOf(t, http.MethodPost, moodLogPath, body), []byte(`{"success":true,"data":{"logId":"stale"}}`),
		).Error)

		res, header := callKey(t, http.MethodPost, moodLogPath, guru.Token, key, body)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
		require.Empty(t, header.Get(idempotency.ReplayedHeader), "an old answer is never replayed")
		require.NotEqual(t, "stale", res.data("logId"))
		require.EqualValues(t, before+1, moodLogCount(t, ameliaID))
	})

	t.Run("the sweep deletes the keys past the retention and keeps the rest", func(t *testing.T) {
		old := uuid.NewString()
		fresh := uuid.NewString()

		for _, key := range []string{old, old + "-b", old + "-c"} {
			require.NoError(t, testDB.Exec(
				"INSERT INTO idempotency_keys (user_id, idempotency_key, fingerprint, status_code, response, created_at) "+
					"VALUES (?, ?, ?, 201, '{}', now() - interval '9 days')",
				guruID, key, []byte{1},
			).Error)
		}

		require.NoError(t, testDB.Exec(
			"INSERT INTO idempotency_keys (user_id, idempotency_key, fingerprint, status_code, response) VALUES (?, ?, ?, 201, '{}')",
			guruID, fresh, []byte{1},
		).Error)

		store := idempotency.New(testDB, 168*time.Hour, 10*time.Second)

		removed, err := store.Sweep(context.Background())
		require.NoError(t, err)
		require.GreaterOrEqual(t, removed, 3)

		require.EqualValues(t, 0, keyRows(t, guruID, old))
		require.EqualValues(t, 0, keyRows(t, guruID, old+"-b"))
		require.EqualValues(t, 0, keyRows(t, guruID, old+"-c"))
		require.EqualValues(t, 1, keyRows(t, guruID, fresh))
	})

	t.Run("a claim of a scan does not spend the daily limit twice", func(t *testing.T) {
		key := uuid.NewString()
		body := map[string]any{"studentId": ameliaID, "trashType": "ORGANIK", "confidenceScore": 0.9}

		first, _ := callKey(t, http.MethodPost, scanClaimPath, amelia.Token, key, body)
		require.Equal(t, http.StatusOK, first.Status, "body %v", first.Body)

		again, header := callKey(t, http.MethodPost, scanClaimPath, amelia.Token, key, body)
		require.Equal(t, http.StatusOK, again.Status, "body %v", again.Body)
		require.Equal(t, "true", header.Get(idempotency.ReplayedHeader))
		require.Equal(t, first.Body, again.Body)

		require.EqualValues(t, 1, scanCount(t, ameliaID), "the repeat must not claim again")
	})

	t.Run("a class and a forum thread are created once", func(t *testing.T) {
		classKey := uuid.NewString()
		classBody := map[string]any{"name": "Kenanga", "gradeLevel": "Class B"}

		first, _ := callKey(t, http.MethodPost, "/api/v1/guru/classes", guru.Token, classKey, classBody)
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)

		again, header := callKey(t, http.MethodPost, "/api/v1/guru/classes", guru.Token, classKey, classBody)
		require.Equal(t, "true", header.Get(idempotency.ReplayedHeader))
		require.Equal(t, first.Body, again.Body)

		classes := call(t, http.MethodGet, "/api/v1/guru/classes", guru.Token, nil)
		require.EqualValues(t, 2, classes.data("total"), "the class of registration and one new class")

		threadKey := uuid.NewString()
		threadBody := map[string]any{"title": "Welcome", "body": "First post of the term"}
		threadPath := "/api/v1/classes/" + classID + "/forum"

		firstThread, _ := callKey(t, http.MethodPost, threadPath, guru.Token, threadKey, threadBody)
		require.Equal(t, http.StatusCreated, firstThread.Status, "body %v", firstThread.Body)

		againThread, threadHeader := callKey(t, http.MethodPost, threadPath, guru.Token, threadKey, threadBody)
		require.Equal(t, "true", threadHeader.Get(idempotency.ReplayedHeader))
		require.Equal(t, firstThread.Body, againThread.Body)

		threads := call(t, http.MethodGet, threadPath, guru.Token, nil)
		require.EqualValues(t, 1, threads.data("total"))
	})
}

// A try that was taken for dead can still finish later. It must neither free nor fill the row of the try that took the key
// over, or a third try would run next to the second one, or the repeat would get the answer of the wrong try.
func TestATryThatWasTakenOverCannotTouchItsReplacement(t *testing.T) {
	guru := registerGuru(t, "Orchid")
	userID := uuid.MustParse(userIDOf(t, guru.Email))
	store := idempotency.New(testDB, time.Hour, time.Second)
	fingerprint := []byte("same request")
	ctx := context.Background()

	row := func(key string) (status *int, response []byte) {
		var found struct {
			StatusCode *int
			Response   []byte
		}

		require.NoError(t, testDB.Raw("SELECT status_code, response FROM idempotency_keys WHERE user_id = ? AND idempotency_key = ?", userID, key).Scan(&found).Error)

		return found.StatusCode, found.Response
	}

	// the first try claims the key and then stalls, so the row counts as abandoned
	takeOver := func(key string) (first uuid.UUID, second uuid.UUID) {
		replayed, first, err := store.Claim(ctx, userID, key, fingerprint)
		require.NoError(t, err)
		require.Nil(t, replayed)

		require.NoError(t, testDB.Exec("UPDATE idempotency_keys SET created_at = now() - interval '1 hour' WHERE user_id = ? AND idempotency_key = ?", userID, key).Error)

		replayed, second, err = store.Claim(ctx, userID, key, fingerprint)
		require.NoError(t, err)
		require.Nil(t, replayed)
		require.NotEqual(t, first, second)

		return first, second
	}

	t.Run("a late failure leaves the claim of the replacement", func(t *testing.T) {
		key := uuid.NewString()
		first, second := takeOver(key)

		store.Release(ctx, userID, key, first)
		require.EqualValues(t, 1, keyRows(t, userID.String(), key), "the replacement keeps its claim")

		// a third try is still told to wait, because the second one is running
		_, _, err := store.Claim(ctx, userID, key, fingerprint)
		require.ErrorIs(t, err, apperror.ErrIdempotencyBusy)

		store.Release(ctx, userID, key, second)
		require.Zero(t, keyRows(t, userID.String(), key), "the owner can free it")
	})

	t.Run("a late success does not fill the row of the replacement", func(t *testing.T) {
		key := uuid.NewString()
		first, second := takeOver(key)

		store.Complete(ctx, userID, key, first, http.StatusCreated, []byte(`{"from":"first"}`))

		status, _ := row(key)
		require.Nil(t, status, "the row still waits for the replacement")

		store.Complete(ctx, userID, key, second, http.StatusCreated, []byte(`{"from":"second"}`))

		status, response := row(key)
		require.NotNil(t, status)
		require.JSONEq(t, `{"from":"second"}`, string(response))

		replayed, _, err := store.Claim(ctx, userID, key, fingerprint)
		require.NoError(t, err)
		require.NotNil(t, replayed)
		require.JSONEq(t, `{"from":"second"}`, string(replayed.Body))
	})
}
