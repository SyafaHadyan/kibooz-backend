// Package e2e exercises the whole HTTP stack against a real PostgreSQL and Redis
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// credentials are generated for every run so no secret-looking value lives in the source
var testPassword = "Pw-" + strings.ReplaceAll(uuid.NewString(), "-", "") + "!"

const (
	testBucket    = "kibooz-test"
	testPublicURL = "https://cdn.example.test"
)

var (
	once    sync.Once
	testApp *fiber.App
	initErr error

	storedMu   sync.Mutex
	storedKeys []string
)

// startMockS3 accepts every PutObject and remembers the object keys, so uploads can be asserted
func startMockS3() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		if r.Method == http.MethodPut {
			storedMu.Lock()
			storedKeys = append(storedKeys, strings.TrimPrefix(r.URL.Path, "/"+testBucket+"/"))
			storedMu.Unlock()
		}

		w.Header().Set("ETag", `"mock"`)
		w.WriteHeader(http.StatusOK)
	}))
}

func uploaded(prefix string) []string {
	storedMu.Lock()
	defer storedMu.Unlock()

	var matches []string

	for _, key := range storedKeys {
		if strings.HasPrefix(key, prefix) {
			matches = append(matches, key)
		}
	}

	return matches
}

func app(t *testing.T) *fiber.App {
	t.Helper()

	if os.Getenv("E2E_ENABLED") != "true" {
		t.Skip("set E2E_ENABLED=true with DB_* and REDIS_* variables to run end to end tests")
	}

	once.Do(func() {
		defaults := map[string]string{
			"LIMITER_MAX":          "100000",
			"AUTH_LIMITER_MAX":     "100000",
			"JWT_SECRET_KEY":       uuid.NewString() + uuid.NewString(),
			"S3_ENDPOINT":          startMockS3().URL,
			"S3_REGION":            "us-east-1",
			"S3_BUCKET_NAME":       testBucket,
			"S3_ACCESS_KEY_ID":     "test",
			"S3_ACCESS_KEY_SECRET": "test",
			"S3_PUBLIC_URL":        testPublicURL,
		}

		for key, value := range defaults {
			_ = os.Setenv(key, value)
		}

		started, err := bootstrap.Start("e2e")
		if err != nil {
			initErr = err

			return
		}

		testApp = started.App.Fiber
	})

	require.NoError(t, initErr)

	return testApp
}

type result struct {
	Status int
	Body   map[string]any
}

func (r result) data(keys ...string) any {
	return dig(r.Body["data"], keys...)
}

func dig(value any, keys ...string) any {
	for _, key := range keys {
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}

		value = m[key]
	}

	return value
}

func call(t *testing.T, method string, path string, token string, body any) result {
	t.Helper()

	return callApp(t, app(t), method, path, token, body)
}

func callApp(t *testing.T, target *fiber.App, method string, path string, token string, body any) result {
	t.Helper()

	var reader *bytes.Reader

	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)

		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := target.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(t, err)

	defer res.Body.Close()

	parsed := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&parsed)

	return result{Status: res.StatusCode, Body: parsed}
}

func (r result) requireError(t *testing.T, status int, code string) {
	t.Helper()

	require.Equal(t, status, r.Status, "body %v", r.Body)
	require.Equal(t, false, r.Body["success"])
	require.Equal(t, code, r.Body["errorCode"])
}

type account struct {
	Token        string
	RefreshToken string
	Email        string
	Password     string
}

func suffix() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

func nisn() string {
	return fmt.Sprintf("%d", 1000000000+uuid.New().ID()%899999999)
}

func registerGuru(t *testing.T, className string) account {
	t.Helper()

	email := fmt.Sprintf("guru.%s@example.com", suffix())

	res := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": testPassword, "fullName": "Siti Rahayu, S.Pd.",
		"role": "GURU", "nip": suffix(), "class": map[string]any{"name": className, "gradeLevel": "Class A"},
	})
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

	return account{
		Token:        res.data("token").(string),
		RefreshToken: res.data("refreshToken").(string),
		Email:        email,
		Password:     testPassword,
	}
}

func registerWali(t *testing.T, classCode string, childName string) (account, string) {
	t.Helper()

	email := fmt.Sprintf("wali.%s@example.com", suffix())

	res := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": testPassword, "fullName": "Sarah Kartika, S.Pd.",
		"role": "WALI", "classCode": classCode, "student": map[string]any{"nisn": nisn(), "fullName": childName},
	})
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

	acc := account{
		Token:        res.data("token").(string),
		RefreshToken: res.data("refreshToken").(string),
		Email:        email,
		Password:     testPassword,
	}

	dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", acc.Token, nil)
	require.Equal(t, http.StatusOK, dashboard.Status, "body %v", dashboard.Body)

	return acc, dashboard.data("student", "id").(string)
}

func classOf(t *testing.T, guru account) (classID string, joinCode string) {
	t.Helper()

	res := call(t, http.MethodGet, "/api/v1/guru/dashboard", guru.Token, nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	return res.data("classOverview", "classId").(string), res.data("classOverview", "joinCode").(string)
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	res, err := app(t).Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	var body map[string]any

	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "ok", dig(body, "checks", "storage"))
}

func TestRegistrationAndLogin(t *testing.T) {
	guru := registerGuru(t, "Mawar")
	_, joinCode := classOf(t, guru)
	require.Len(t, joinCode, 6)

	t.Run("duplicate email", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": strings.ToUpper(guru.Email), "password": testPassword, "fullName": "Dobel",
			"role": "GURU", "class": map[string]any{"name": "Melati"},
		})
		res.requireError(t, http.StatusConflict, "EMAIL_ALREADY_REGISTERED")
	})

	t.Run("unknown class code", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("x.%s@example.com", suffix()), "password": testPassword, "fullName": "New Parent",
			"role": "WALI", "classCode": "ZZZZZZ", "student": map[string]any{"nisn": nisn(), "fullName": "Anak"},
		})
		res.requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")
	})

	t.Run("duplicate nisn", func(t *testing.T) {
		sharedNISN := nisn()
		body := func() map[string]any {
			return map[string]any{
				"email": fmt.Sprintf("n.%s@example.com", suffix()), "password": testPassword, "fullName": "Wali",
				"role": "WALI", "classCode": strings.ToLower(joinCode), "student": map[string]any{"nisn": sharedNISN, "fullName": "Anak"},
			}
		}

		require.Equal(t, http.StatusCreated, call(t, http.MethodPost, "/api/v1/auth/register", "", body()).Status)
		call(t, http.MethodPost, "/api/v1/auth/register", "", body()).requireError(t, http.StatusConflict, "NISN_ALREADY_REGISTERED")
	})

	t.Run("validation", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": "not-an-email", "password": "short", "fullName": "A", "role": "ADMIN",
		})
		res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		details := res.Body["details"].(map[string]any)
		require.Contains(t, details, "email")
		require.Contains(t, details, "password")
		require.Contains(t, details, "role")

		missingClass := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("g.%s@example.com", suffix()), "password": testPassword, "fullName": "Guru", "role": "GURU",
		})
		missingClass.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		require.Contains(t, missingClass.Body["details"], "class")
	})

	t.Run("login success and failures", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": guru.Email, "password": guru.Password, "role": "GURU",
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, true, res.Body["success"])
		require.Equal(t, "Login successful", res.Body["message"])
		require.Equal(t, "GURU", res.data("user", "role"))
		require.NotEmpty(t, res.data("token"))
		require.NotContains(t, res.Body["data"].(map[string]any)["user"], "passwordHash")

		call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": guru.Email, "password": "salah-total", "role": "GURU",
		}).requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")

		call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": guru.Email, "password": guru.Password, "role": "WALI",
		}).requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")

		call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": "not.found@example.com", "password": testPassword, "role": "GURU",
		}).requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
	})
}

func TestRefreshTokenRotationAndLogout(t *testing.T) {
	guru := registerGuru(t, "Anggrek")

	refreshed := call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": guru.RefreshToken})
	require.Equal(t, http.StatusOK, refreshed.Status, "body %v", refreshed.Body)

	newRefresh := refreshed.data("refreshToken").(string)
	require.NotEqual(t, guru.RefreshToken, newRefresh)

	// the consumed token cannot be replayed
	call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": guru.RefreshToken}).
		requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")

	// the new access token works
	require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/guru/dashboard", refreshed.data("token").(string), nil).Status)

	require.Equal(t, http.StatusOK, call(t, http.MethodPost, "/api/v1/auth/logout", "", map[string]any{"refreshToken": newRefresh}).Status)
	call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": newRefresh}).
		requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
}

func TestAccessControl(t *testing.T) {
	guru := registerGuru(t, "Dahlia")
	_, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Dahlia Child")

	call(t, http.MethodGet, "/api/v1/guru/dashboard", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	call(t, http.MethodGet, "/api/v1/guru/dashboard", "not.a.valid.token", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_INVALID")
	call(t, http.MethodGet, "/api/v1/guru/dashboard", wali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	call(t, http.MethodGet, "/api/v1/wali/dashboard", guru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	call(t, http.MethodPost, "/api/v1/trash/scan-claim", guru.Token, map[string]any{}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	call(t, http.MethodGet, "/api/v1/not-found", "", nil).requireError(t, http.StatusNotFound, "NOT_FOUND")
}

func TestMoodAndDashboards(t *testing.T) {
	guru := registerGuru(t, "Mawar")
	classID, joinCode := classOf(t, guru)

	amelia, ameliaID := registerWali(t, joinCode, "Amelia Siti Zahra")
	_, farhanID := registerWali(t, joinCode, "Farhan Al-Fatih")

	otherGuru := registerGuru(t, "Kenanga")

	t.Run("no mood yet", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil)
		require.Equal(t, http.StatusOK, res.Status)
		require.Nil(t, res.data("todayMood"))
		require.Nil(t, res.data("recommendedGuidance"))
		require.Equal(t, "Amelia Siti Zahra", res.data("student", "fullName"))
		require.Contains(t, res.data("student", "className"), "Class A")
	})

	t.Run("log mood", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": ameliaID, "moodType": "BINGUNG", "source": "AI_CAMERA", "confidenceScore": 0.88,
			"notes": "Hesitant when parting at the gate.",
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
		require.NotEmpty(t, res.data("logId"))
		require.NotEmpty(t, res.data("recordedAt"))

		manual := call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": farhanID, "moodType": "SENANG",
		})
		require.Equal(t, http.StatusCreated, manual.Status, "body %v", manual.Body)
	})

	t.Run("mood validation and ownership", func(t *testing.T) {
		call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": ameliaID, "moodType": "LAPAR",
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": ameliaID, "moodType": "SENANG", "source": "AI_CAMERA",
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": ameliaID, "moodType": "SENANG", "confidenceScore": 1.5,
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": uuid.NewString(), "moodType": "SENANG",
		}).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")

		// a teacher of another class must not write into this class
		call(t, http.MethodPost, "/api/v1/guru/mood/log", otherGuru.Token, map[string]any{
			"studentId": ameliaID, "moodType": "SENANG",
		}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	})

	t.Run("wali dashboard shows today's mood", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil)
		require.Equal(t, http.StatusOK, res.Status)
		require.Equal(t, "BINGUNG", res.data("todayMood", "moodType"))
		require.Equal(t, "Unsure / Needs Support", res.data("todayMood", "label"))
		require.InDelta(t, 0.88, res.data("todayMood", "confidence"), 0.001)
		require.Equal(t, "Hesitant when parting at the gate.", res.data("todayMood", "teacherNotes"))
		require.Equal(t, "guidance-bingung-4step", res.data("recommendedGuidance", "id"))
		require.Equal(t, "Give Your Child a Sense of Safety", res.data("recommendedGuidance", "title"))
	})

	t.Run("wali cannot read someone else's child", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+farhanID, amelia.Token, nil).
			requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId=not-a-uuid", amelia.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("guru dashboard", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/guru/dashboard", guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status)
		require.EqualValues(t, 2, res.data("classOverview", "totalStudents"))
		require.EqualValues(t, 2, res.data("classOverview", "presentStudents"))
		require.Equal(t, "Class A (Mawar)", res.data("classOverview", "className"))
		require.EqualValues(t, 1, res.data("dailyMoodDistribution", "senang"))
		require.EqualValues(t, 1, res.data("dailyMoodDistribution", "bingung"))
		require.EqualValues(t, 0, res.data("dailyMoodDistribution", "marah"))
		require.Equal(t, "SENANG", res.data("classOverview", "dominantMood"))

		call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+classID, guru.Token, nil)
		call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+uuid.NewString(), guru.Token, nil).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")

		empty := call(t, http.MethodGet, "/api/v1/guru/dashboard", otherGuru.Token, nil)
		require.EqualValues(t, 0, empty.data("classOverview", "totalStudents"))
		require.Nil(t, empty.data("classOverview", "dominantMood"))
	})

	t.Run("analytics", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/guru/mood/analytics?classId="+classID+"&range=weekly", guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		total := 0.0
		for _, slice := range res.data("donutSummary").([]any) {
			total += slice.(map[string]any)["percentage"].(float64)
		}

		require.InDelta(t, 100, total, 0.001)
		require.Len(t, res.data("weeklyTrend"), 5)
		require.Nil(t, res.data("monthlyDistribution"))

		monthly := call(t, http.MethodGet, "/api/v1/guru/mood/analytics?classId="+classID+"&range=monthly", guru.Token, nil)
		require.Equal(t, http.StatusOK, monthly.Status)
		require.Len(t, monthly.data("monthlyDistribution"), 4)

		call(t, http.MethodGet, "/api/v1/guru/mood/analytics?range=yearly", guru.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/guru/mood/analytics?classId="+classID, otherGuru.Token, nil).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
	})

	t.Run("apply guidance", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/wali/guidance/apply", amelia.Token, map[string]any{
			"guidanceId": "guidance-bingung-4step", "studentId": ameliaID, "parentNotes": "Already talked to gently.",
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Handling status forwarded to the class teacher", res.Body["message"])

		call(t, http.MethodPost, "/api/v1/wali/guidance/apply", amelia.Token, map[string]any{
			"guidanceId": "not-found", "studentId": ameliaID,
		}).requireError(t, http.StatusNotFound, "GUIDANCE_NOT_FOUND")

		call(t, http.MethodPost, "/api/v1/wali/guidance/apply", amelia.Token, map[string]any{
			"guidanceId": "guidance-bingung-4step", "studentId": farhanID,
		}).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})
}

func TestTrashClaimsAndLeaderboard(t *testing.T) {
	guru := registerGuru(t, "Mawar")
	classID, joinCode := classOf(t, guru)

	amelia, ameliaID := registerWali(t, joinCode, "Amelia Siti Zahra")
	farhan, farhanID := registerWali(t, joinCode, "Farhan Al-Fatih")

	outsiderGuru := registerGuru(t, "Lain")
	outsiderClassID, outsiderCode := classOf(t, outsiderGuru)
	outsider, _ := registerWali(t, outsiderCode, "Other Class Child")

	claim := func(token string, studentID string, trashType string) result {
		return call(t, http.MethodPost, "/api/v1/trash/scan-claim", token, map[string]any{
			"studentId": studentID, "trashType": trashType, "confidenceScore": 0.91,
		})
	}

	t.Run("points and rank follow the business rules", func(t *testing.T) {
		first := claim(amelia.Token, ameliaID, "ANORGANIK")
		require.Equal(t, http.StatusOK, first.Status, "body %v", first.Body)
		require.EqualValues(t, 15, first.data("pointsAdded"))
		require.EqualValues(t, 15, first.data("totalPoints"))
		require.EqualValues(t, 1, first.data("newRank"))
		require.EqualValues(t, 4, first.data("remainingDailyScans"))

		second := claim(amelia.Token, ameliaID, "ORGANIK")
		require.EqualValues(t, 10, second.data("pointsAdded"))
		require.EqualValues(t, 25, second.data("totalPoints"))
		require.EqualValues(t, 3, second.data("remainingDailyScans"))

		third := claim(farhan.Token, farhanID, "ORGANIK")
		require.EqualValues(t, 10, third.data("totalPoints"))
		require.EqualValues(t, 2, third.data("newRank"))

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil)
		require.EqualValues(t, 25, dashboard.data("pointsSummary", "totalPoints"))
		require.EqualValues(t, 1, dashboard.data("pointsSummary", "classRank"))
		require.EqualValues(t, 1, dashboard.data("pointsSummary", "organicCount"))
		require.EqualValues(t, 1, dashboard.data("pointsSummary", "anorganicCount"))
	})

	t.Run("leaderboard is cached and refreshed after a claim", func(t *testing.T) {
		before := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, amelia.Token, nil)
		require.Equal(t, http.StatusOK, before.Status, "body %v", before.Body)

		podium := before.data("podium").([]any)
		require.Len(t, podium, 2)
		require.Equal(t, "Amelia Siti Zahra", dig(podium[0], "studentName"))
		require.EqualValues(t, 25, dig(podium[0], "points"))
		require.EqualValues(t, 1, dig(podium[0], "rank"))
		require.Equal(t, "Farhan Al-Fatih", dig(podium[1], "studentName"))
		require.Empty(t, before.data("rankings"))

		repeat := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, farhan.Token, nil)
		require.Equal(t, before.Body, repeat.Body)

		// Farhan overtakes Amelia, which must invalidate the cached ranking
		for range 3 {
			require.Equal(t, http.StatusOK, claim(farhan.Token, farhanID, "ANORGANIK").Status)
		}

		after := call(t, http.MethodGet, "/api/v1/leaderboard", farhan.Token, nil)
		afterPodium := after.data("podium").([]any)
		require.Equal(t, "Farhan Al-Fatih", dig(afterPodium[0], "studentName"))
		require.EqualValues(t, 55, dig(afterPodium[0], "points"))

		// the teacher sees the same class without passing classId
		teacherView := call(t, http.MethodGet, "/api/v1/leaderboard", guru.Token, nil)
		require.Equal(t, after.Body, teacherView.Body)
	})

	t.Run("leaderboard access is limited to the class", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, outsider.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, outsiderGuru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/leaderboard?classId="+outsiderClassID, outsider.Token, nil).Status)
		call(t, http.MethodGet, "/api/v1/leaderboard?classId=not-a-uuid", amelia.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("daily limit of five claims", func(t *testing.T) {
		// Amelia already used two claims
		for i := range 3 {
			res := claim(amelia.Token, ameliaID, "ORGANIK")
			require.Equal(t, http.StatusOK, res.Status, "claim %d body %v", i, res.Body)
		}

		claim(amelia.Token, ameliaID, "ORGANIK").requireError(t, http.StatusTooManyRequests, "TRASH_DAILY_LIMIT_REACHED")

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil)
		require.EqualValues(t, 55, dashboard.data("pointsSummary", "totalPoints"))
	})

	t.Run("validation and ownership", func(t *testing.T) {
		claim(outsider.Token, ameliaID, "ORGANIK").requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")

		call(t, http.MethodPost, "/api/v1/trash/scan-claim", farhan.Token, map[string]any{
			"studentId": farhanID, "trashType": "KACA", "confidenceScore": 0.9,
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		call(t, http.MethodPost, "/api/v1/trash/scan-claim", farhan.Token, map[string]any{
			"studentId": farhanID, "trashType": "ORGANIK", "confidenceScore": 2,
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		call(t, http.MethodPost, "/api/v1/trash/scan-claim", farhan.Token, map[string]any{
			"studentId": farhanID, "trashType": "ORGANIK", "confidenceScore": 0.9, "photoBase64": "data:image/png;base64,@@@",
		}).requireError(t, http.StatusBadRequest, "INVALID_IMAGE")
	})

	t.Run("photo is uploaded as evidence", func(t *testing.T) {
		outsiderStudent := dashboardStudentID(t, outsider)

		res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", outsider.Token, map[string]any{
			"studentId": outsiderStudent, "trashType": "B3", "confidenceScore": 0.8,
			"photoBase64": "data:image/png;base64," + tinyPNG,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 0, res.data("pointsAdded"))

		keys := uploaded("trash-scans/" + outsiderStudent + "/")
		require.Len(t, keys, 1)
		require.True(t, strings.HasSuffix(keys[0], ".png"))
	})
}

func dashboardStudentID(t *testing.T, wali account) string {
	t.Helper()

	return call(t, http.MethodGet, "/api/v1/wali/dashboard", wali.Token, nil).data("student", "id").(string)
}

func TestConcurrentClaimsNeverExceedDailyLimit(t *testing.T) {
	guru := registerGuru(t, "Konkuren")
	_, joinCode := classOf(t, guru)
	wali, studentID := registerWali(t, joinCode, "Fast Child")

	const attempts = 12

	statuses := make(chan int, attempts)

	var wg sync.WaitGroup

	for range attempts {
		wg.Add(1)

		go func() {
			defer wg.Done()

			res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", wali.Token, map[string]any{
				"studentId": studentID, "trashType": "ORGANIK", "confidenceScore": 0.95,
			})

			statuses <- res.Status
		}()
	}

	wg.Wait()
	close(statuses)

	succeeded, limited := 0, 0

	for status := range statuses {
		switch status {
		case http.StatusOK:
			succeeded++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected status %d", status)
		}
	}

	require.Equal(t, 5, succeeded)
	require.Equal(t, attempts-5, limited)

	dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", wali.Token, nil)
	require.EqualValues(t, 50, dashboard.data("pointsSummary", "totalPoints"))
}

func upload(t *testing.T, token string, fields map[string]string, filename string, content []byte) result {
	t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)

	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}

	if filename != "" {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
		header.Set("Content-Type", "application/octet-stream")

		part, err := writer.CreatePart(header)
		require.NoError(t, err)

		_, err = part.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := app(t).Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(t, err)

	defer res.Body.Close()

	parsed := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&parsed)

	return result{Status: res.StatusCode, Body: parsed}
}

func TestAvatarUpload(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	require.NoError(t, err)

	guru := registerGuru(t, "Avatar")
	classID, joinCode := classOf(t, guru)
	wali, studentID := registerWali(t, joinCode, "Child With Photo")
	otherWali, _ := registerWali(t, joinCode, "Another Child")

	t.Run("user avatar", func(t *testing.T) {
		res := upload(t, guru.Token, nil, "foto.png", png)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		url := res.data("avatarUrl").(string)
		require.True(t, strings.HasPrefix(url, testPublicURL+"/avatars/"), url)
		require.True(t, strings.HasSuffix(url, ".png"))

		login := call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": guru.Email, "password": guru.Password, "role": "GURU",
		})
		require.Equal(t, url, login.data("user", "avatarUrl"))

		dashboard := call(t, http.MethodGet, "/api/v1/guru/dashboard", guru.Token, nil)
		require.Equal(t, url, dashboard.data("teacher", "avatarUrl"))
	})

	t.Run("student avatar refreshes the cached leaderboard", func(t *testing.T) {
		before := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, wali.Token, nil)
		require.Equal(t, http.StatusOK, before.Status)

		res := upload(t, wali.Token, map[string]string{"studentId": studentID}, "anak.png", png)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		url := res.data("avatarUrl").(string)
		require.Contains(t, url, "/avatars/"+studentID+"/")

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", wali.Token, nil)
		require.Equal(t, url, dashboard.data("student", "avatarUrl"))

		after := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, wali.Token, nil)
		avatars := map[string]bool{}

		for _, entry := range after.data("podium").([]any) {
			if avatar, ok := dig(entry, "avatarUrl").(string); ok {
				avatars[avatar] = true
			}
		}

		require.True(t, avatars[url], "leaderboard %v", after.Body)
	})

	t.Run("rejections", func(t *testing.T) {
		upload(t, wali.Token, map[string]string{"studentId": studentID}, "x.png", png)

		upload(t, otherWali.Token, map[string]string{"studentId": studentID}, "x.png", png).
			requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		upload(t, guru.Token, map[string]string{"studentId": studentID}, "x.png", png).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		upload(t, wali.Token, nil, "notes.png", []byte("plain text pretending to be an image")).
			requireError(t, http.StatusBadRequest, "INVALID_IMAGE")
		upload(t, wali.Token, nil, "", nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		upload(t, wali.Token, map[string]string{"studentId": "not-a-uuid"}, "x.png", png).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		upload(t, wali.Token, nil, "besar.png", append(png, make([]byte, 3*1024*1024)...)).
			requireError(t, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE")
	})
}

func redisClient(t *testing.T) *goredis.Client {
	t.Helper()

	client := goredis.NewClient(&goredis.Options{
		Addr: os.Getenv("REDIS_ADDRESS") + ":" + os.Getenv("REDIS_PORT"),
	})

	t.Cleanup(func() { _ = client.Close() })

	return client
}

func refresh(t *testing.T, token string) result {
	t.Helper()

	return call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": token})
}

func TestRefreshTokensDoNotDependOnRedis(t *testing.T) {
	ctx := context.Background()
	client := redisClient(t)

	t.Run("a token still works after Redis loses its data", func(t *testing.T) {
		guru := registerGuru(t, "Redis Lost")

		require.NoError(t, client.FlushAll(ctx).Err())

		res := refresh(t, guru.RefreshToken)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		// the old token is gone from Postgres, so it cannot be replayed even though Redis forgot it
		require.NoError(t, client.FlushAll(ctx).Err())
		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
	})

	t.Run("a replay is rejected while Redis remembers the token", func(t *testing.T) {
		guru := registerGuru(t, "Replay")

		first := refresh(t, guru.RefreshToken)
		require.Equal(t, http.StatusOK, first.Status)

		sum := sha256.Sum256([]byte(guru.RefreshToken))
		used, err := client.Get(ctx, "refresh:"+hex.EncodeToString(sum[:])).Result()
		require.NoError(t, err)
		require.Equal(t, "used", used)

		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
	})

	t.Run("a stale Redis key cannot revive a revoked token", func(t *testing.T) {
		guru := registerGuru(t, "Dicabut")

		require.Equal(t, http.StatusOK, call(t, http.MethodPost, "/api/v1/auth/logout", "", map[string]any{"refreshToken": guru.RefreshToken}).Status)

		sum := sha256.Sum256([]byte(guru.RefreshToken))
		require.NoError(t, client.Set(ctx, "refresh:"+hex.EncodeToString(sum[:]), uuid.NewString(), time.Hour).Err())

		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
	})

	t.Run("logout works when Redis has no record", func(t *testing.T) {
		guru := registerGuru(t, "Logout")

		require.NoError(t, client.FlushAll(ctx).Err())
		require.Equal(t, http.StatusOK, call(t, http.MethodPost, "/api/v1/auth/logout", "", map[string]any{"refreshToken": guru.RefreshToken}).Status)

		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
	})

	t.Run("tokens are stored hashed", func(t *testing.T) {
		guru := registerGuru(t, "Hash")
		require.NotContains(t, guru.RefreshToken, " ")
		require.Len(t, guru.RefreshToken, 36)
	})
}

func TestAPIWorksWithoutRedis(t *testing.T) {
	app(t) // makes sure the shared environment defaults are set

	// port 1 never has a listener, which is how an unreachable Redis looks
	t.Setenv("REDIS_PORT", "1")

	started, err := bootstrap.Start("e2e-no-redis")
	require.NoError(t, err)

	t.Cleanup(started.Close)

	noRedis := started.App.Fiber

	health := callApp(t, noRedis, http.MethodGet, "/healthz", "", nil)
	require.Equal(t, http.StatusOK, health.Status, "body %v", health.Body)
	require.Equal(t, "degraded", health.Body["status"])
	require.Equal(t, "down", dig(health.Body, "checks", "redis"))
	require.Equal(t, "ok", dig(health.Body, "checks", "database"))

	email := fmt.Sprintf("guru.%s@example.com", suffix())

	registered := callApp(t, noRedis, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": testPassword, "fullName": "Teacher Without Redis",
		"role": "GURU", "class": map[string]any{"name": "Without Redis"},
	})
	require.Equal(t, http.StatusCreated, registered.Status, "body %v", registered.Body)

	token := registered.data("token").(string)
	refreshToken := registered.data("refreshToken").(string)

	login := callApp(t, noRedis, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": email, "password": testPassword, "role": "GURU",
	})
	require.Equal(t, http.StatusOK, login.Status, "body %v", login.Body)

	rotated := callApp(t, noRedis, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": refreshToken})
	require.Equal(t, http.StatusOK, rotated.Status, "body %v", rotated.Body)

	callApp(t, noRedis, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": refreshToken}).
		requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")

	dashboard := callApp(t, noRedis, http.MethodGet, "/api/v1/guru/dashboard", token, nil)
	require.Equal(t, http.StatusOK, dashboard.Status, "body %v", dashboard.Body)

	leaderboard := callApp(t, noRedis, http.MethodGet, "/api/v1/leaderboard", token, nil)
	require.Equal(t, http.StatusOK, leaderboard.Status, "body %v", leaderboard.Body)

	require.Equal(t, http.StatusOK, callApp(t, noRedis, http.MethodPost, "/api/v1/auth/logout", "", map[string]any{
		"refreshToken": rotated.data("refreshToken"),
	}).Status)
}

func registerWaliWith(t *testing.T, email string, studentNISN string, classCode string, childName string) result {
	t.Helper()

	return call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": testPassword, "fullName": "Re-registered Parent",
		"role": "WALI", "classCode": classCode, "student": map[string]any{"nisn": studentNISN, "fullName": childName},
	})
}

func deleteAccount(t *testing.T, token string, password string) result {
	t.Helper()

	return call(t, http.MethodDelete, "/api/v1/users/me", token, map[string]any{"password": password})
}

// requireLockedOut checks that an access token of a deleted account, which stays valid until it expires, reaches nothing
func requireLockedOut(t *testing.T, token string, classID string) {
	t.Helper()

	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	require.NoError(t, err)

	storedBefore := len(uploaded(""))

	attempts := map[string]result{
		"wali dashboard":       call(t, http.MethodGet, "/api/v1/wali/dashboard", token, nil),
		"guru dashboard":       call(t, http.MethodGet, "/api/v1/guru/dashboard", token, nil),
		"leaderboard":          call(t, http.MethodGet, "/api/v1/leaderboard", token, nil),
		"leaderboard of class": call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, token, nil),
		"avatar upload":        upload(t, token, nil, "foto.png", png),
		"delete account again": deleteAccount(t, token, testPassword),
		"mood log":             call(t, http.MethodPost, "/api/v1/guru/mood/log", token, map[string]any{"studentId": uuid.NewString(), "moodType": "SENANG"}),
		"scan claim":           call(t, http.MethodPost, "/api/v1/trash/scan-claim", token, map[string]any{"studentId": uuid.NewString(), "trashType": "ORGANIK", "confidenceScore": 0.9}),
	}

	for name, res := range attempts {
		require.GreaterOrEqual(t, res.Status, http.StatusBadRequest, "%s must be refused, body %v", name, res.Body)
	}

	require.Len(t, uploaded(""), storedBefore, "a deleted account must not leave an uploaded file")
}

func TestAccountSoftDelete(t *testing.T) {
	guru := registerGuru(t, "Anggrek")
	classID, joinCode := classOf(t, guru)

	amelia, ameliaID := registerWali(t, joinCode, "Amelia Siti Zahra")
	farhan, farhanID := registerWali(t, joinCode, "Farhan Al-Fatih")

	claim := func(token string, studentID string) {
		res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", token, map[string]any{
			"studentId": studentID, "trashType": "ANORGANIK", "confidenceScore": 0.9,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	}

	claim(amelia.Token, ameliaID)
	claim(amelia.Token, ameliaID)
	claim(farhan.Token, farhanID)

	require.EqualValues(t, 2, call(t, http.MethodGet, "/api/v1/wali/dashboard", farhan.Token, nil).data("pointsSummary", "classRank"))
	require.EqualValues(t, 2, call(t, http.MethodGet, "/api/v1/guru/dashboard", guru.Token, nil).data("classOverview", "totalStudents"))

	t.Run("a wrong or missing password deletes nothing", func(t *testing.T) {
		deleteAccount(t, amelia.Token, "not-the-password").requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")
		call(t, http.MethodDelete, "/api/v1/users/me", amelia.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		deleteAccount(t, "", testPassword).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")

		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil).Status)
	})

	t.Run("deleting a parent hides the account and the child", func(t *testing.T) {
		res := deleteAccount(t, amelia.Token, testPassword)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		// no new session, however it is asked for
		call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": amelia.Email, "password": testPassword, "role": "WALI",
		}).requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
		refresh(t, amelia.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")

		// the access token that is still valid no longer reaches any data
		requireLockedOut(t, amelia.Token, classID)
	})

	t.Run("the class forgets the child and the ranking closes the gap", func(t *testing.T) {
		require.EqualValues(t, 1, call(t, http.MethodGet, "/api/v1/wali/dashboard", farhan.Token, nil).data("pointsSummary", "classRank"))
		require.EqualValues(t, 1, call(t, http.MethodGet, "/api/v1/guru/dashboard", guru.Token, nil).data("classOverview", "totalStudents"))

		board := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, guru.Token, nil)
		require.Equal(t, http.StatusOK, board.Status, "body %v", board.Body)

		podium := board.data("podium").([]any)
		require.Len(t, podium, 1)
		require.Equal(t, "Farhan Al-Fatih", dig(podium[0], "studentName"))
		require.EqualValues(t, 1, dig(podium[0], "rank"))

		// the teacher can no longer record a mood for the hidden child
		call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": ameliaID, "moodType": "SENANG",
		}).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})

	t.Run("the email and the NISN can be registered again", func(t *testing.T) {
		reuseEmail := fmt.Sprintf("wali.%s@example.com", suffix())
		reuseNISN := nisn()

		first := registerWaliWith(t, reuseEmail, reuseNISN, joinCode, "First Child")
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)

		// still unique among active accounts
		registerWaliWith(t, reuseEmail, nisn(), joinCode, "Same Email").requireError(t, http.StatusConflict, "EMAIL_ALREADY_REGISTERED")
		registerWaliWith(t, fmt.Sprintf("wali.%s@example.com", suffix()), reuseNISN, joinCode, "Same NISN").
			requireError(t, http.StatusConflict, "NISN_ALREADY_REGISTERED")

		require.Equal(t, http.StatusOK, deleteAccount(t, first.data("token").(string), testPassword).Status)

		again := registerWaliWith(t, reuseEmail, reuseNISN, joinCode, "Returning Child")
		require.Equal(t, http.StatusCreated, again.Status, "body %v", again.Body)
	})

	t.Run("deleting a teacher ends their sessions and frees the email and NIP", func(t *testing.T) {
		teacher := registerGuru(t, "Melati")
		teacherClassID, _ := classOf(t, teacher)

		res := deleteAccount(t, teacher.Token, testPassword)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": teacher.Email, "password": testPassword, "role": "GURU",
		}).requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
		refresh(t, teacher.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
		requireLockedOut(t, teacher.Token, teacherClassID)

		again := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": teacher.Email, "password": testPassword, "fullName": "Returning Teacher",
			"role": "GURU", "class": map[string]any{"name": "New Melati", "gradeLevel": "Class B"},
		})
		require.Equal(t, http.StatusCreated, again.Status, "body %v", again.Body)
	})
}
