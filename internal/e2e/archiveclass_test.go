package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTheOwnerArchivesAClassAndAnyTeacherRestoresIt(t *testing.T) {
	owner := registerGuru(t, "Melur")
	classID, oldCode := classOf(t, owner)
	partner := registerGuru(t, "Nusa Indah")
	stranger := registerGuru(t, "Oleander")
	strangerClass, strangerCode := classOf(t, stranger)
	parent, childID := registerWali(t, oldCode, "Anak Kelas Diarsipkan")
	mover, moverID := registerWali(t, strangerCode, "Anak Yang Ingin Pindah")

	base := "/api/v1/guru/classes/" + classID

	added := call(t, http.MethodPost, base+"/teachers", owner.Token, map[string]any{"email": partner.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	archive := func(token string) result { return call(t, http.MethodPost, base+"/archive", token, nil) }
	restore := func(token string) result { return call(t, http.MethodPost, base+"/restore", token, nil) }

	t.Run("only the owner archives", func(t *testing.T) {
		archive(partner.Token).requireError(t, http.StatusForbidden, "CLASS_OWNER_REQUIRED")
		archive(stranger.Token).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		archive(parent.Token).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		archive("").requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodPost, "/api/v1/guru/classes/not-a-uuid/archive", owner.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, "/api/v1/guru/classes/"+uuid.NewString()+"/archive", owner.Token, nil).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")

		restore(owner.Token).requireError(t, http.StatusConflict, "CLASS_NOT_ARCHIVED")
	})

	waiting := requestMove(t, mover.Token, moverID, oldCode)
	require.Equal(t, http.StatusCreated, waiting.Status, "body %v", waiting.Body)

	claimScan(t, parent.Token, childID, "ORGANIK")

	t.Run("the class is read only once the owner archives it", func(t *testing.T) {
		res := archive(owner.Token)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Class archived", res.Body["message"])
		require.NotEmpty(t, res.data("archivedAt"))
		require.Equal(t, oldCode, res.data("joinCode"), "archiving keeps the code, it just stops working")

		archive(owner.Token).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")

		call(t, http.MethodPost, "/api/v1/trash/scan-claim", parent.Token, map[string]any{
			"studentId": childID, "trashType": "ORGANIK", "confidenceScore": 0.9,
		}).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")

		registerWaliWith(t, fmt.Sprintf("late.%s@example.com", suffix()), nisn(), oldCode, "Anak Terlambat").
			requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")

		// the request that waited was rejected by the database and the child of the other class stays where it is
		list := call(t, http.MethodGet, "/api/v1/wali/children/"+moverID+"/move-requests", mover.Token, nil)
		require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)

		requests, ok := list.data("requests").([]any)
		require.True(t, ok)
		require.Len(t, requests, 1)
		require.Equal(t, "rejected", dig(requests[0], "status"))

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+moverID, mover.Token, nil)
		require.Equal(t, strangerClass, dashboard.data("student", "classId"))
	})

	t.Run("any teacher restores it with a new join code", func(t *testing.T) {
		restore(stranger.Token).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		restore(parent.Token).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		res := restore(partner.Token)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Class restored", res.Body["message"])
		require.Nil(t, res.data("archivedAt"), "a restored class is not archived any more")

		newCode, ok := res.data("joinCode").(string)
		require.True(t, ok)
		require.NotEqual(t, oldCode, newCode, "the code is rotated, so the old one does not come back to life")

		restore(owner.Token).requireError(t, http.StatusConflict, "CLASS_NOT_ARCHIVED")

		registerWaliWith(t, fmt.Sprintf("old.%s@example.com", suffix()), nisn(), oldCode, "Anak Kode Lama").
			requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")

		fresh := registerWaliWith(t, fmt.Sprintf("new.%s@example.com", suffix()), nisn(), newCode, "Anak Kode Baru")
		require.Equal(t, http.StatusCreated, fresh.Status, "body %v", fresh.Body)

		claimScan(t, parent.Token, childID, "ORGANIK")
	})
}

func TestARestoredClassCountsTowardTheLimitOfTwentyClasses(t *testing.T) {
	teacher := registerGuru(t, "Pandan")
	classID, _ := classOf(t, teacher)

	createClass := func(name string) result {
		return call(t, http.MethodPost, "/api/v1/guru/classes", teacher.Token, map[string]any{"name": name})
	}

	// the class of the registration and 18 more are 19 classes, and the archived one is not counted
	for i := 1; i <= 18; i++ {
		res := createClass(fmt.Sprintf("Kelas Batas %d", i))
		require.Equal(t, http.StatusCreated, res.Status, "class %d body %v", i, res.Body)
	}

	archived := call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/archive", teacher.Token, nil)
	require.Equal(t, http.StatusOK, archived.Status, "body %v", archived.Body)

	// 18 classes are left that are not archived, so two more fit and then the limit is reached
	require.Equal(t, http.StatusCreated, createClass("Kelas Batas 19").Status)
	require.Equal(t, http.StatusCreated, createClass("Kelas Batas 20").Status)

	call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/restore", teacher.Token, nil).
		requireError(t, http.StatusConflict, "CLASS_LIMIT_REACHED")
}
