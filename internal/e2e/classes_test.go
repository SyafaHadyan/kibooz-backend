package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTeacherClasses(t *testing.T) {
	guru := registerGuru(t, "Sakura")
	firstID, firstCode := classOf(t, guru)
	wali, _ := registerWali(t, firstCode, "Sakura Child")

	other := registerGuru(t, "Sedap Malam")
	otherID, _ := classOf(t, other)

	t.Run("the class from registration is listed with its children", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/guru/classes", guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 1, res.data("total"))
		require.EqualValues(t, 1, res.data("page"))
		require.EqualValues(t, 20, res.data("limit"))

		classes := listOf(t, res, "classes")
		require.Len(t, classes, 1)
		require.Equal(t, firstID, dig(classes[0], "id"))
		require.Equal(t, firstCode, dig(classes[0], "joinCode"))
		require.Equal(t, "Sakura", dig(classes[0], "name"))
		require.Equal(t, "Class A", dig(classes[0], "gradeLevel"))
		require.EqualValues(t, 1, dig(classes[0], "totalStudents"))
	})

	var melatiID, melatiCode string

	t.Run("create classes", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/guru/classes", guru.Token, map[string]any{
			"name": " Melati ", "gradeLevel": "Class B", "academicYear": "2027/2028", "schoolName": "TK Harapan Baru",
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
		require.Equal(t, "Class created", res.Body["message"])
		require.Equal(t, "Melati", res.data("name"))
		require.Equal(t, "Class B", res.data("gradeLevel"))
		require.Equal(t, "2027/2028", res.data("academicYear"))
		require.Equal(t, "TK Harapan Baru", res.data("schoolName"))
		require.EqualValues(t, 0, res.data("totalStudents"))
		require.Len(t, res.data("joinCode"), 6)
		require.NotEqual(t, firstCode, res.data("joinCode"))

		melatiID, melatiCode = res.data("id").(string), res.data("joinCode").(string)

		defaults := call(t, http.MethodPost, "/api/v1/guru/classes", guru.Token, map[string]any{"name": "Tulip"})
		require.Equal(t, http.StatusCreated, defaults.Status, "body %v", defaults.Body)
		require.Equal(t, "Class A", defaults.data("gradeLevel"))
		require.Equal(t, "2026/2027", defaults.data("academicYear"))
		require.Equal(t, "TK Pertiwi Harapan Bangsa", defaults.data("schoolName"))
	})

	t.Run("a class needs a name", func(t *testing.T) {
		for _, body := range []map[string]any{{}, {"name": "   "}, {"name": strings.Repeat("a", 51)}, {"name": "A", "gradeLevel": strings.Repeat("a", 21)}} {
			call(t, http.MethodPost, "/api/v1/guru/classes", guru.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}
	})

	t.Run("a parent registers a child with the code of a new class", func(t *testing.T) {
		_, childID := registerWali(t, melatiCode, "Melati Child")
		require.NotEmpty(t, childID)

		res := call(t, http.MethodGet, "/api/v1/guru/classes/"+melatiID, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 1, res.data("totalStudents"))
	})

	t.Run("classes come oldest first and page", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/guru/classes", guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 3, res.data("total"))
		require.Equal(t, []string{"Sakura", "Melati", "Tulip"}, titles(t, listOf(t, res, "classes"), "name"))

		second := call(t, http.MethodGet, "/api/v1/guru/classes?page=2&limit=1", guru.Token, nil)
		require.Equal(t, http.StatusOK, second.Status, "body %v", second.Body)
		require.Equal(t, []string{"Melati"}, titles(t, listOf(t, second, "classes"), "name"))

		call(t, http.MethodGet, "/api/v1/guru/classes?limit=0", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a class shows how much content it holds", func(t *testing.T) {
		empty := call(t, http.MethodGet, "/api/v1/guru/classes/"+firstID, guru.Token, nil)
		require.Equal(t, http.StatusOK, empty.Status, "body %v", empty.Body)
		require.EqualValues(t, 0, empty.data("totalVideos"))
		require.EqualValues(t, 0, empty.data("totalThreads"))

		video := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/videos", firstID), guru.Token,
			map[string]any{"title": "Sorting", "videoUrl": "https://videos.example.com/a.mp4"})
		require.Equal(t, http.StatusCreated, video.Status, "body %v", video.Body)

		thread := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/forum", firstID), wali.Token,
			map[string]any{"title": "Hello", "body": "Hello everyone"})
		require.Equal(t, http.StatusCreated, thread.Status, "body %v", thread.Body)

		reply := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/forum/%s/replies", firstID, thread.data("id")), guru.Token,
			map[string]any{"body": "Welcome"})
		require.Equal(t, http.StatusCreated, reply.Status, "body %v", reply.Body)

		res := call(t, http.MethodGet, "/api/v1/guru/classes/"+firstID, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 1, res.data("totalVideos"))
		require.EqualValues(t, 1, res.data("totalThreads"), "a reply is not a thread")
		require.EqualValues(t, 1, res.data("totalStudents"))
		require.Equal(t, firstCode, res.data("joinCode"))
	})

	t.Run("a class of another teacher is not reachable", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/guru/classes/"+otherID, guru.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/guru/classes/"+uuid.NewString(), guru.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/guru/classes/not-a-uuid", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("only teachers manage classes", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/guru/classes", wali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, "/api/v1/guru/classes", wali.Token, map[string]any{"name": "No"}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/guru/classes/"+firstID, wali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/guru/classes", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})
}

func TestTeacherProfile(t *testing.T) {
	guru := registerGuru(t, "Anyelir")
	_, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Anyelir Child")

	t.Run("the profile shows the identity", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/guru/profile", guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.NotEmpty(t, res.data("id"))
		require.Equal(t, "Siti Rahayu, S.Pd.", res.data("fullName"))
		require.NotEmpty(t, res.data("nip"))
		require.Equal(t, "TK Pertiwi Harapan Bangsa", res.data("schoolName"))
		require.Nil(t, res.data("avatarUrl"))
	})

	t.Run("the detail starts without contact details", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/guru/profile/detail", guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, guru.Email, res.data("email"))
		require.Nil(t, res.data("phoneNumber"))
		require.Nil(t, res.data("address"))
		require.NotEmpty(t, res.data("joinedAt"))
	})

	t.Run("change the phone number and the address", func(t *testing.T) {
		res := call(t, http.MethodPut, "/api/v1/guru/profile", guru.Token, map[string]any{
			"phoneNumber": " +62 812-3456-7890 ", "address": " Jl. Merdeka 1, Malang ",
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Profile updated", res.Body["message"])
		require.Equal(t, "+62 812-3456-7890", res.data("phoneNumber"))
		require.Equal(t, "Jl. Merdeka 1, Malang", res.data("address"))

		detail := call(t, http.MethodGet, "/api/v1/guru/profile/detail", guru.Token, nil)
		require.Equal(t, "+62 812-3456-7890", detail.data("phoneNumber"))
		require.Equal(t, "Jl. Merdeka 1, Malang", detail.data("address"))
	})

	t.Run("a missing field stays and an empty text clears", func(t *testing.T) {
		onlyAddress := call(t, http.MethodPut, "/api/v1/guru/profile", guru.Token, map[string]any{"address": "Jl. Veteran 2"})
		require.Equal(t, http.StatusOK, onlyAddress.Status, "body %v", onlyAddress.Body)
		require.Equal(t, "+62 812-3456-7890", onlyAddress.data("phoneNumber"))
		require.Equal(t, "Jl. Veteran 2", onlyAddress.data("address"))

		cleared := call(t, http.MethodPut, "/api/v1/guru/profile", guru.Token, map[string]any{"phoneNumber": ""})
		require.Equal(t, http.StatusOK, cleared.Status, "body %v", cleared.Body)
		require.Nil(t, cleared.data("phoneNumber"))
		require.Equal(t, "Jl. Veteran 2", cleared.data("address"))

		both := call(t, http.MethodPut, "/api/v1/guru/profile", guru.Token, map[string]any{"address": "  "})
		require.Equal(t, http.StatusOK, both.Status, "body %v", both.Body)
		require.Nil(t, both.data("address"))
	})

	t.Run("invalid changes are refused", func(t *testing.T) {
		for _, body := range []map[string]any{
			{},
			{"phoneNumber": "abc"},
			{"phoneNumber": "12345"},
			{"phoneNumber": strings.Repeat("1", 31)},
			{"address": strings.Repeat("a", 1001)},
		} {
			call(t, http.MethodPut, "/api/v1/guru/profile", guru.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}
	})

	t.Run("only a teacher uses the teacher profile", func(t *testing.T) {
		for _, path := range []string{"/api/v1/guru/profile", "/api/v1/guru/profile/detail"} {
			call(t, http.MethodGet, path, wali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
			call(t, http.MethodGet, path, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		}

		call(t, http.MethodPut, "/api/v1/guru/profile", wali.Token, map[string]any{"address": "x"}).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	})
}

func TestClassRoster(t *testing.T) {
	guru := registerGuru(t, "Kamboja")
	classID, joinCode := classOf(t, guru)

	_, zahraID := registerWali(t, joinCode, "Zahra Putri")
	zainal, _ := registerWali(t, joinCode, "Zainal Abidin")
	_, ameliaID := registerWali(t, joinCode, "Amelia Siti")

	otherGuru := registerGuru(t, "Kemuning")
	otherWali, _ := registerWali(t, func() string { _, code := classOf(t, otherGuru); return code }(), "Other Child")

	roster := fmt.Sprintf("/api/v1/classes/%s/students", classID)

	require.Equal(t, http.StatusCreated, call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token,
		map[string]any{"studentId": ameliaID, "moodType": "SEDIH"}).Status)

	t.Run("children are sorted by name with the latest mood of today", func(t *testing.T) {
		res := call(t, http.MethodGet, roster, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 3, res.data("total"))
		require.Equal(t, []string{"Amelia Siti", "Zahra Putri", "Zainal Abidin"}, titles(t, listOf(t, res, "students"), "fullName"))

		students := listOf(t, res, "students")
		require.Equal(t, ameliaID, dig(students[0], "id"))
		require.Equal(t, "SEDIH", dig(students[0], "todayMood"))
		require.Nil(t, dig(students[1], "todayMood"))
		require.Equal(t, zahraID, dig(students[1], "id"))
		require.Regexp(t, `^\d+$`, dig(students[0], "nisn"))
		require.EqualValues(t, 0, dig(students[0], "currentPoints"))
	})

	t.Run("paging", func(t *testing.T) {
		res := call(t, http.MethodGet, roster+"?page=2&limit=2", guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, []string{"Zainal Abidin"}, titles(t, listOf(t, res, "students"), "fullName"))

		call(t, http.MethodGet, roster+"?page=0", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("only the teachers of the class see the register", func(t *testing.T) {
		call(t, http.MethodGet, roster, zainal.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, roster, otherGuru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, roster, otherWali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, roster, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodGet, "/api/v1/classes/not-a-uuid/students", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a deleted child leaves the register", func(t *testing.T) {
		require.Equal(t, http.StatusOK, deleteAccount(t, zainal.Token, zainal.Password).Status)

		res := call(t, http.MethodGet, roster, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 2, res.data("total"))
		require.Equal(t, []string{"Amelia Siti", "Zahra Putri"}, titles(t, listOf(t, res, "students"), "fullName"))
	})
}

func TestChildProfile(t *testing.T) {
	guru := registerGuru(t, "Cendana")
	classID, joinCode := classOf(t, guru)

	email := fmt.Sprintf("wali.%s@example.com", suffix())
	childNISN := nisn()

	registered := call(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": testPassword, "fullName": "Sarah Kartika", "role": "WALI",
		"phoneNumber": "081234567890", "whatsappNumber": "+6281234567890", "address": "Jl. Kenanga 5, Malang",
		"classCode": joinCode, "student": map[string]any{"nisn": childNISN, "fullName": "Amelia Siti Zahra"},
	})
	require.Equal(t, http.StatusCreated, registered.Status, "body %v", registered.Body)

	token := registered.data("token").(string)
	dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", token, nil)
	childID := dashboard.data("student", "id").(string)

	outsider, outsiderChild := registerWali(t, joinCode, "Another Child")

	t.Run("the child shows the registered details", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/wali/child/"+childID, token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, childID, res.data("id"))
		require.Equal(t, "Amelia Siti Zahra", res.data("fullName"))
		require.Equal(t, childNISN, res.data("nisn"))
		require.Nil(t, res.data("avatarUrl"))
		require.Equal(t, classID, res.data("classId"))
		require.Equal(t, "Cendana", res.data("className"))
		require.Equal(t, "Class A", res.data("gradeLevel"))
		require.Equal(t, "TK Pertiwi Harapan Bangsa", res.data("schoolName"))
		require.Equal(t, "2026/2027", res.data("academicYear"))
		require.EqualValues(t, 0, res.data("currentPoints"))

		require.Equal(t, "Sarah Kartika", res.data("guardian", "fullName"))
		require.Equal(t, email, res.data("guardian", "email"))
		require.Equal(t, "081234567890", res.data("guardian", "phoneNumber"))
		require.Equal(t, "+6281234567890", res.data("guardian", "whatsappNumber"))
		require.Equal(t, "Jl. Kenanga 5, Malang", res.data("guardian", "address"))
	})

	t.Run("a parent without contact details gets nulls", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/wali/child/"+outsiderChild, outsider.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Nil(t, res.data("guardian", "phoneNumber"))
		require.Nil(t, res.data("guardian", "whatsappNumber"))
		require.Nil(t, res.data("guardian", "address"))
	})

	t.Run("only the parent of the child sees it", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/wali/child/"+childID, outsider.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/wali/child/"+uuid.NewString(), token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/wali/child/not-a-uuid", token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/wali/child/"+childID, guru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/wali/child/"+childID, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})
}

func TestTrashStatistics(t *testing.T) {
	guru := registerGuru(t, "Dahlia Putih")
	_, joinCode := classOf(t, guru)

	wali, childID := registerWali(t, joinCode, "Amelia Siti Zahra")
	other, otherChild := registerWali(t, joinCode, "Farhan Al-Fatih")

	claim := func(trashType string) int {
		res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", wali.Token, map[string]any{
			"studentId": childID, "trashType": trashType, "confidenceScore": 0.9,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		return int(res.data("pointsAdded").(float64))
	}

	categories := func(res result) map[string]map[string]float64 {
		out := map[string]map[string]float64{}

		for _, item := range listOf(t, res, "breakdown") {
			entry := item.(map[string]any)
			out[entry["trashType"].(string)] = map[string]float64{"scans": entry["scans"].(float64), "points": entry["points"].(float64)}
		}

		return out
	}

	t.Run("a child without scans has zero everywhere", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/trash/stats", wali.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, childID, res.data("student", "id"))
		require.Equal(t, "Amelia Siti Zahra", res.data("student", "fullName"))
		require.EqualValues(t, 0, res.data("totalPoints"))
		require.EqualValues(t, 0, res.data("totalScans"))
		require.EqualValues(t, 0, res.data("todayScans"))
		require.Equal(t, res.data("dailyLimit"), res.data("remainingDailyScans"))
		require.Equal(t, []string{"ORGANIK", "ANORGANIK", "B3"}, titles(t, listOf(t, res, "breakdown"), "trashType"))
	})

	t.Run("scans and points add up for each category", func(t *testing.T) {
		organic := claim("ORGANIK") + claim("ORGANIK")
		inorganic := claim("ANORGANIK")
		hazardous := claim("B3")

		res := call(t, http.MethodGet, "/api/v1/trash/stats?studentId="+childID, wali.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, organic+inorganic+hazardous, res.data("totalPoints"))
		require.EqualValues(t, 4, res.data("totalScans"))
		require.EqualValues(t, 4, res.data("todayScans"))
		require.EqualValues(t, res.data("dailyLimit").(float64)-4, res.data("remainingDailyScans"))
		require.EqualValues(t, 1, res.data("classRank"))

		stats := categories(res)
		require.EqualValues(t, 2, stats["ORGANIK"]["scans"])
		require.EqualValues(t, organic, stats["ORGANIK"]["points"])
		require.EqualValues(t, 1, stats["ANORGANIK"]["scans"])
		require.EqualValues(t, inorganic, stats["ANORGANIK"]["points"])
		require.EqualValues(t, 1, stats["B3"]["scans"])
		require.EqualValues(t, hazardous, stats["B3"]["points"])
	})

	t.Run("another child is not reachable", func(t *testing.T) {
		own := call(t, http.MethodGet, "/api/v1/trash/stats", other.Token, nil)
		require.Equal(t, http.StatusOK, own.Status, "body %v", own.Body)
		require.Equal(t, otherChild, own.data("student", "id"))
		require.EqualValues(t, 0, own.data("totalScans"))

		call(t, http.MethodGet, "/api/v1/trash/stats?studentId="+childID, other.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/trash/stats?studentId=not-a-uuid", wali.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/trash/stats", guru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/trash/stats", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})
}
