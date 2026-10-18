package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/classteam"
)

func archivedAt(t *testing.T, classID string) *string {
	t.Helper()

	var found struct{ ArchivedAt *string }

	require.NoError(t, testDB.Raw("SELECT archived_at::text AS archived_at FROM classes WHERE id = ?", classID).Scan(&found).Error)

	return found.ArchivedAt
}

func TestAClassWithoutChildrenIsArchivedWhenItsLastTeacherLeaves(t *testing.T) {
	teacher := registerGuru(t, "Anggrek Kosong")
	emptyID, emptyCode := classOf(t, teacher)

	extra := call(t, http.MethodPost, "/api/v1/guru/classes", teacher.Token, map[string]any{"name": "Kelas Dua"})
	require.Equal(t, http.StatusCreated, extra.Status, "body %v", extra.Body)

	secondID, _ := extra.data("id").(string)

	parent, _ := registerWali(t, emptyCode, "Leaves Early")
	require.Equal(t, http.StatusOK, deleteAccount(t, parent.Token, parent.Password).Status, "the only child is deleted with the parent")

	t.Run("a class whose children are all deleted still counts as empty", func(t *testing.T) {
		deleteAccount(t, teacher.Token, "not-the-password").requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")
		require.Nil(t, archivedAt(t, emptyID), "nothing is archived by a refused deletion")

		res := deleteAccount(t, teacher.Token, teacher.Password)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		require.NotNil(t, archivedAt(t, emptyID))
		require.NotNil(t, archivedAt(t, secondID))
	})

	t.Run("the join code of an archived class is not found", func(t *testing.T) {
		registerWaliWith(t, fmt.Sprintf("wali.%s@example.com", suffix()), nisn(), emptyCode, "Late Child").
			requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")

		other := registerGuru(t, "Kelas Lain")
		_, otherCode := classOf(t, other)
		keeper, _ := registerWali(t, otherCode, "Keeps Going")

		res := call(t, http.MethodPost, "/api/v1/wali/children", keeper.Token, map[string]any{
			"classCode": emptyCode, "student": map[string]any{"nisn": nisn(), "fullName": "Second Child"},
		})
		res.requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")
	})
}

func TestAClassWithChildrenStillNeedsASuccessor(t *testing.T) {
	teacher := registerGuru(t, "Melati Ramai")
	successor := registerGuru(t, "Penerus")
	fullID, fullCode := classOf(t, teacher)
	registerWali(t, fullCode, "Stays Here")

	extra := call(t, http.MethodPost, "/api/v1/guru/classes", teacher.Token, map[string]any{"name": "Kelas Kosong"})
	require.Equal(t, http.StatusCreated, extra.Status, "body %v", extra.Body)

	emptyID, _ := extra.data("id").(string)

	deleteAccount(t, teacher.Token, teacher.Password).requireError(t, http.StatusConflict, "SUCCESSOR_REQUIRED")
	require.Nil(t, archivedAt(t, emptyID), "the empty class is not archived when the deletion is refused")
	require.Nil(t, archivedAt(t, fullID))

	res := deleteAccountTo(t, teacher.Token, teacher.Password, successor.Email)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	require.Nil(t, archivedAt(t, emptyID), "a named successor takes every class, empty ones too")
	require.Nil(t, archivedAt(t, fullID))

	list := call(t, http.MethodGet, "/api/v1/guru/classes", successor.Token, nil)
	require.Len(t, listOf(t, list, "classes"), 3)
}

func TestASharedClassIsNotArchivedWhenOneTeacherLeaves(t *testing.T) {
	staying := registerGuru(t, "Tetap")
	leaving := registerGuru(t, "Pergi")
	classID, _ := classOf(t, staying)

	// taken before the shared class is added, because the dashboard shows the oldest class of the teacher
	ownID, _ := classOf(t, leaving)

	added := call(t, http.MethodPost, fmt.Sprintf("/api/v1/guru/classes/%s/teachers", classID), staying.Token, map[string]any{"email": leaving.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	// the class of the one who leaves has nobody else, so only that one is archived
	require.Equal(t, http.StatusOK, deleteAccount(t, leaving.Token, leaving.Password).Status)

	require.Nil(t, archivedAt(t, classID))
	require.NotNil(t, archivedAt(t, ownID))
}

// A parent who joins at the moment the last teacher leaves either gets in, and then the deletion needs a successor, or
// finds the class archived. A class must never be archived with a child in it.
func TestAChildJoiningWhileTheLastTeacherLeavesNeverEndsInAnArchivedClass(t *testing.T) {
	for round := range 6 {
		teacher := registerGuru(t, fmt.Sprintf("Balapan %d", round))
		classID, code := classOf(t, teacher)

		var (
			wg       sync.WaitGroup
			joined   result
			deletion result
		)

		wg.Add(2)

		go func() {
			defer wg.Done()

			joined = registerWaliWith(t, fmt.Sprintf("wali.%s@example.com", suffix()), nisn(), code, "Racer")
		}()

		go func() {
			defer wg.Done()

			deletion = deleteAccount(t, teacher.Token, teacher.Password)
		}()

		wg.Wait()

		var children int64

		require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM students WHERE class_id = ? AND deleted_at IS NULL", classID).Scan(&children).Error)

		if archivedAt(t, classID) != nil {
			require.Zero(t, children, "round %d, an archived class holds a child", round)
			require.Equal(t, http.StatusOK, deletion.Status, "round %d, body %v", round, deletion.Body)
			joined.requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")

			continue
		}

		// the child got in first, or the teacher had already been refused
		require.EqualValues(t, 1, children, "round %d", round)
		require.Equal(t, http.StatusCreated, joined.Status, "round %d, body %v", round, joined.Body)
		deletion.requireError(t, http.StatusConflict, "SUCCESSOR_REQUIRED")
	}
}

// A join that looked the class up just before it was archived waits for the lock and then has to find it archived. The
// race itself is too narrow to hit on purpose, so the check after the lock is made on its own.
func TestLockOpenRefusesAnArchivedClass(t *testing.T) {
	teacher := registerGuru(t, "Terkunci")
	classID, _ := classOf(t, teacher)
	id := uuid.MustParse(classID)

	require.NoError(t, testDB.Transaction(func(tx *gorm.DB) error { return classteam.LockOpen(tx, id) }), "an open class is locked")

	require.NoError(t, testDB.Exec("UPDATE classes SET archived_at = now() WHERE id = ?", classID).Error)

	err := testDB.Transaction(func(tx *gorm.DB) error { return classteam.LockOpen(tx, id) })
	require.ErrorIs(t, err, apperror.ErrClassCodeNotFound)
}

// A lock that cannot be taken is an error of the caller's transaction and not an answer about the class
func TestLockOpenReportsAFailingTransaction(t *testing.T) {
	app(t)

	tx := testDB.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Rollback().Error)

	err := classteam.LockOpen(tx, uuid.New())
	require.Error(t, err)
	require.NotErrorIs(t, err, apperror.ErrClassCodeNotFound)
}
