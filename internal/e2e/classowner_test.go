package e2e

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ownerOf reads the teachers of the class as the caller sees them, in the order they joined, and returns the id of the one who owns it
func ownerOf(t *testing.T, token string, classID string) (owner string, count int) {
	t.Helper()

	res := call(t, http.MethodGet, fmt.Sprintf("/api/v1/guru/classes/%s/teachers", classID), token, nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	owners := 0

	for _, item := range listOf(t, res, "teachers") {
		if dig(item, "isOwner") == true {
			owner, _ = dig(item, "id").(string)
			owners++
		}
	}

	require.LessOrEqual(t, owners, 1, "a class never has two owners")

	return owner, len(listOf(t, res, "teachers"))
}

func TestTheOwnerOfAClassManagesItsTeachers(t *testing.T) {
	first := registerGuru(t, "Kamboja")
	second := registerGuru(t, "Lavender")
	third := registerGuru(t, "Magnolia")
	outsider := registerGuru(t, "Nusa Indah")
	classID, joinCode := classOf(t, first)

	base := fmt.Sprintf("/api/v1/guru/classes/%s", classID)
	remove := func(token string, teacherID string) result {
		return call(t, http.MethodDelete, base+"/teachers/"+teacherID, token, nil)
	}
	give := func(token string, teacherID string) result {
		return call(t, http.MethodPut, base+"/owner", token, map[string]any{"teacherId": teacherID})
	}

	initial := call(t, http.MethodGet, base+"/teachers", first.Token, nil)
	firstID, _ := dig(listOf(t, initial, "teachers")[0], "id").(string)
	require.NotEmpty(t, firstID)
	require.Equal(t, true, dig(listOf(t, initial, "teachers")[0], "isOwner"), "the teacher who made the class owns it")

	added := call(t, http.MethodPost, base+"/teachers", first.Token, map[string]any{"email": second.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)
	require.Equal(t, false, added.data("isOwner"), "an added teacher is not the owner")

	secondID, _ := added.data("id").(string)

	added = call(t, http.MethodPost, base+"/teachers", first.Token, map[string]any{"email": third.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	thirdID, _ := added.data("id").(string)

	t.Run("a teacher who is not the owner cannot remove others or hand the class over", func(t *testing.T) {
		remove(second.Token, thirdID).requireError(t, http.StatusForbidden, "CLASS_OWNER_REQUIRED")
		remove(second.Token, firstID).requireError(t, http.StatusForbidden, "CLASS_OWNER_REQUIRED")
		give(second.Token, secondID).requireError(t, http.StatusForbidden, "CLASS_OWNER_REQUIRED")
	})

	t.Run("people outside the class cannot touch it", func(t *testing.T) {
		remove(outsider.Token, firstID).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		give(outsider.Token, firstID).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodDelete, base+"/teachers/"+firstID, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")

		parent, _ := registerWali(t, joinCode, "Anak Wali")
		remove(parent.Token, firstID).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		give(parent.Token, firstID).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	})

	t.Run("a bad or unknown teacher is refused", func(t *testing.T) {
		remove(first.Token, "not-a-uuid").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		remove(first.Token, uuid.NewString()).requireError(t, http.StatusNotFound, "TEACHER_NOT_IN_CLASS")
		give(first.Token, uuid.NewString()).requireError(t, http.StatusNotFound, "TEACHER_NOT_IN_CLASS")
		call(t, http.MethodPut, base+"/owner", first.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodDelete, "/api/v1/guru/classes/not-a-uuid/teachers/"+firstID, first.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPut, "/api/v1/guru/classes/not-a-uuid/owner", first.Token, map[string]any{"teacherId": firstID}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("the owner hands the class to another teacher", func(t *testing.T) {
		same := give(first.Token, firstID)
		require.Equal(t, http.StatusOK, same.Status, "body %v", same.Body)
		require.Equal(t, true, same.data("isOwner"), "naming the owner again changes nothing")

		res := give(first.Token, secondID)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, secondID, res.data("id"))
		require.Equal(t, true, res.data("isOwner"))

		owner, count := ownerOf(t, second.Token, classID)
		require.Equal(t, secondID, owner)
		require.Equal(t, 3, count, "the old owner stays a teacher")

		remove(first.Token, thirdID).requireError(t, http.StatusForbidden, "CLASS_OWNER_REQUIRED")
		give(first.Token, firstID).requireError(t, http.StatusForbidden, "CLASS_OWNER_REQUIRED")
	})

	t.Run("the owner removes a teacher who then loses the class", func(t *testing.T) {
		res := remove(second.Token, thirdID)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		call(t, http.MethodGet, base+"/teachers", third.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/classes/"+classID+"/students", third.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		remove(second.Token, thirdID).requireError(t, http.StatusNotFound, "TEACHER_NOT_IN_CLASS")

		_, count := ownerOf(t, second.Token, classID)
		require.Equal(t, 2, count)
	})

	t.Run("the owner who leaves hands the class to the teacher who joined next", func(t *testing.T) {
		res := remove(second.Token, secondID)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		owner, count := ownerOf(t, first.Token, classID)
		require.Equal(t, firstID, owner)
		require.Equal(t, 1, count)

		call(t, http.MethodGet, base+"/teachers", second.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
	})

	t.Run("the only teacher cannot leave", func(t *testing.T) {
		remove(first.Token, firstID).requireError(t, http.StatusConflict, "LAST_TEACHER")

		owner, count := ownerOf(t, first.Token, classID)
		require.Equal(t, firstID, owner)
		require.Equal(t, 1, count)
	})

	t.Run("a teacher who is not the owner can leave", func(t *testing.T) {
		added := call(t, http.MethodPost, base+"/teachers", first.Token, map[string]any{"email": third.Email})
		require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

		again, _ := added.data("id").(string)

		require.Equal(t, http.StatusOK, remove(third.Token, again).Status)

		owner, count := ownerOf(t, first.Token, classID)
		require.Equal(t, firstID, owner, "the owner is the same")
		require.Equal(t, 1, count)
	})
}

func TestDeletingTheAccountOfTheOwnerHandsTheClassOver(t *testing.T) {
	owner := registerGuru(t, "Orchid")
	next := registerGuru(t, "Peony")
	classID, _ := classOf(t, owner)

	base := fmt.Sprintf("/api/v1/guru/classes/%s", classID)

	added := call(t, http.MethodPost, base+"/teachers", owner.Token, map[string]any{"email": next.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	nextID, _ := added.data("id").(string)

	res := deleteAccount(t, owner.Token, owner.Password)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	who, count := ownerOf(t, next.Token, classID)
	require.Equal(t, nextID, who, "the teacher who is left owns the class")
	require.Equal(t, 1, count, "the deleted account is not listed")

	// a deleted teacher that is not the owner changes nothing
	third := registerGuru(t, "Rose")
	require.Equal(t, http.StatusCreated, call(t, http.MethodPost, base+"/teachers", next.Token, map[string]any{"email": third.Email}).Status)
	require.Equal(t, http.StatusOK, deleteTeacher(t, third).Status)

	who, count = ownerOf(t, next.Token, classID)
	require.Equal(t, nextID, who)
	require.Equal(t, 1, count)
}

// TestTheMigrationNamesTheFirstActiveTeacherOwner runs the backfill of the migration on every class of a rolled back
// transaction, after all owners were taken away, which is what a database that was filled before owners existed looks like
func TestTheMigrationNamesTheFirstActiveTeacherOwner(t *testing.T) {
	app(t) // makes sure the database is ready

	script, err := os.ReadFile("../infra/db/migrations/000013_class_owner.up.sql")
	require.NoError(t, err)

	start := strings.Index(string(script), "UPDATE class_teachers AS ct")
	require.GreaterOrEqual(t, start, 0)

	backfill := string(script)[start:]
	backfill = backfill[:strings.Index(backfill, ";")+1]

	owner := registerGuru(t, "Tulip")
	second := registerGuru(t, "Violet")
	classID, _ := classOf(t, owner)

	added := call(t, http.MethodPost, fmt.Sprintf("/api/v1/guru/classes/%s/teachers", classID), owner.Token, map[string]any{"email": second.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	secondID, _ := added.data("id").(string)

	// the first teacher deletes the account, so the class keeps a deleted teacher who joined first
	require.Equal(t, http.StatusOK, deleteAccount(t, owner.Token, owner.Password).Status)

	tx := testDB.Begin()
	require.NoError(t, tx.Error)

	t.Cleanup(func() { _ = tx.Rollback().Error })

	require.NoError(t, tx.Exec("UPDATE class_teachers SET is_owner = false").Error)
	require.NoError(t, tx.Exec(backfill).Error)

	var owners []string

	require.NoError(t, tx.Raw("SELECT guru_id::text FROM class_teachers WHERE class_id = ?::uuid AND is_owner", classID).Scan(&owners).Error)
	require.Equal(t, []string{secondID}, owners, "the deleted teacher who joined first is skipped")

	var twice int64

	require.NoError(t, tx.Raw(
		"SELECT count(*) FROM (SELECT class_id FROM class_teachers WHERE is_owner GROUP BY class_id HAVING count(*) > 1) AS many",
	).Scan(&twice).Error)
	require.Zero(t, twice, "no class got two owners")
}

// A teacher who deletes the account while another teacher hands the class to them must never leave the class with a
// deleted owner, because nobody could remove teachers or hand the class on after that
func TestHandingAClassToATeacherWhoIsDeletingTheAccountNeverLeavesNoOwner(t *testing.T) {
	for round := range 6 {
		owner := registerGuru(t, fmt.Sprintf("Cempaka %d", round))
		leaving := registerGuru(t, fmt.Sprintf("Dahlia %d", round))
		classID, _ := classOf(t, owner)
		base := fmt.Sprintf("/api/v1/guru/classes/%s", classID)

		added := call(t, http.MethodPost, base+"/teachers", owner.Token, map[string]any{"email": leaving.Email})
		require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

		leavingID, _ := added.data("id").(string)
		ownerID, _ := ownerOf(t, owner.Token, classID)

		var wg sync.WaitGroup

		var gave, deleted result

		wg.Add(2)

		go func() {
			defer wg.Done()

			gave = call(t, http.MethodPut, base+"/owner", owner.Token, map[string]any{"teacherId": leavingID})
		}()

		go func() {
			defer wg.Done()

			// the class that the account made has no other teacher, so the owner takes it
			deleted = deleteAccountTo(t, leaving.Token, leaving.Password, owner.Email)
		}()

		wg.Wait()

		require.Equal(t, http.StatusOK, deleted.Status, "body %v", deleted.Body)
		require.Contains(t, []int{http.StatusOK, http.StatusNotFound}, gave.Status, "round %d, body %v", round, gave.Body)

		who, count := ownerOf(t, owner.Token, classID)
		require.Equal(t, ownerID, who, "round %d, the one teacher who is left owns the class", round)
		require.Equal(t, 1, count)
	}
}
