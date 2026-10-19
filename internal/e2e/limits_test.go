package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

func TestATeacherCanTeachAtMost20Classes(t *testing.T) {
	guru := registerGuru(t, "Kelas Satu")
	createClass := func(token string, name string) result {
		return call(t, http.MethodPost, "/api/v1/guru/classes", token, map[string]any{"name": name})
	}

	for i := 2; i <= constants.MaxClassesPerTeacher; i++ {
		res := createClass(guru.Token, fmt.Sprintf("Kelas %d", i))
		require.Equal(t, http.StatusCreated, res.Status, "class %d body %v", i, res.Body)
	}

	createClass(guru.Token, "Terlalu Banyak").requireError(t, http.StatusConflict, "CLASS_LIMIT_REACHED")

	t.Run("a teacher who already teaches 20 classes cannot be added to another one", func(t *testing.T) {
		other := registerGuru(t, "Kelas Lain")
		otherClassID, _ := classOf(t, other)

		res := call(t, http.MethodPost, fmt.Sprintf("/api/v1/guru/classes/%s/teachers", otherClassID), other.Token, map[string]any{"email": guru.Email})
		res.requireError(t, http.StatusConflict, "CLASS_LIMIT_REACHED")
	})

	t.Run("a teacher below the limit is not affected", func(t *testing.T) {
		other := registerGuru(t, "Kelas Aman")
		require.Equal(t, http.StatusCreated, createClass(other.Token, "Kelas Dua").Status)
	})
}

func TestAClassHolds60Children(t *testing.T) {
	guru := registerGuru(t, "Kelas Penuh")
	classID, joinCode := classOf(t, guru)
	parent, _ := registerWali(t, joinCode, "Anak Pertama")

	var waliID string

	require.NoError(t, testDB.Raw(
		"SELECT w.id FROM walis w JOIN users u ON u.id = w.user_id WHERE u.email = ?", parent.Email,
	).Scan(&waliID).Error)

	// the children are written straight to the table, because registering 59 parents would only test bcrypt
	students := make([]entity.Student, 0, constants.MaxChildrenPerClass-1)
	for i := 1; i < constants.MaxChildrenPerClass; i++ {
		students = append(students, entity.Student{
			ID: uuid.New(), WaliID: uuid.MustParse(waliID), ClassID: uuid.MustParse(classID),
			NISN: "lim" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20], FullName: fmt.Sprintf("Anak Massal %d", i),
		})
	}

	require.NoError(t, testDB.CreateInBatches(&students, 30).Error)

	t.Run("a new parent cannot join a full class", func(t *testing.T) {
		res := registerWaliWith(t, fmt.Sprintf("wali.%s@example.com", suffix()), nisn(), joinCode, "Anak Ke-61")
		res.requireError(t, http.StatusConflict, "CLASS_FULL")
	})

	t.Run("a parent cannot add a child to a full class", func(t *testing.T) {
		other, _ := registerWali(t, otherClassCode(t), "Anak Lain")
		res := call(t, http.MethodPost, "/api/v1/wali/children", other.Token, map[string]any{
			"classCode": joinCode, "student": map[string]any{"nisn": nisn(), "fullName": "Anak Ke-61"},
		})
		res.requireError(t, http.StatusConflict, "CLASS_FULL")
	})

	t.Run("a place frees up when a child leaves", func(t *testing.T) {
		require.NoError(t, testDB.Exec("UPDATE students SET deleted_at = now() WHERE id = ?", students[0].ID).Error)

		res := registerWaliWith(t, fmt.Sprintf("wali.%s@example.com", suffix()), nisn(), joinCode, "Anak Pengganti")
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
	})
}

// otherClassCode makes a class of its own and returns its join code
func otherClassCode(t *testing.T) string {
	t.Helper()

	_, code := classOf(t, registerGuru(t, "Kelas Sendiri"))

	return code
}
