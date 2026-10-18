package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheOnlyTeacherOfAClassNamesASuccessorToDeleteTheAccount(t *testing.T) {
	teacher := registerGuru(t, "Anyelir")
	successor := registerGuru(t, "Bakung")
	classID, joinCode := classOf(t, teacher)
	wali, _ := registerWali(t, joinCode, "Child Of Anyelir")

	t.Run("a deletion without a successor is refused and changes nothing", func(t *testing.T) {
		deleteAccount(t, teacher.Token, teacher.Password).requireError(t, http.StatusConflict, "SUCCESSOR_REQUIRED")
		deleteAccountTo(t, teacher.Token, teacher.Password, "").requireError(t, http.StatusConflict, "SUCCESSOR_REQUIRED")

		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/guru/dashboard", teacher.Token, nil).Status)
		_, count := ownerOf(t, teacher.Token, classID)
		require.Equal(t, 1, count)
	})

	t.Run("a successor that cannot take the class is refused", func(t *testing.T) {
		deleteAccountTo(t, teacher.Token, teacher.Password, fmt.Sprintf("nobody.%s@example.com", suffix())).
			requireError(t, http.StatusNotFound, "TEACHER_NOT_FOUND")
		deleteAccountTo(t, teacher.Token, teacher.Password, wali.Email).requireError(t, http.StatusNotFound, "TEACHER_NOT_FOUND")
		deleteAccountTo(t, teacher.Token, teacher.Password, teacher.Email).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		deleteAccountTo(t, teacher.Token, teacher.Password, "not-an-email").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		gone := registerGuru(t, "Cempaka Gugur")
		require.Equal(t, http.StatusOK, deleteTeacher(t, gone).Status)
		deleteAccountTo(t, teacher.Token, teacher.Password, gone.Email).requireError(t, http.StatusNotFound, "TEACHER_NOT_FOUND")

		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/guru/dashboard", teacher.Token, nil).Status)
	})

	t.Run("the password is still checked first", func(t *testing.T) {
		deleteAccountTo(t, teacher.Token, "not-the-password", successor.Email).requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")
	})

	t.Run("the successor takes the class as its owner", func(t *testing.T) {
		res := deleteAccountTo(t, teacher.Token, teacher.Password, strings.ToUpper(successor.Email))
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		list := call(t, http.MethodGet, fmt.Sprintf("/api/v1/guru/classes/%s/teachers", classID), successor.Token, nil)
		require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)

		teachers := listOf(t, list, "teachers")
		require.Len(t, teachers, 1)
		require.Equal(t, true, dig(teachers[0], "isOwner"))

		students := call(t, http.MethodGet, fmt.Sprintf("/api/v1/classes/%s/students", classID), successor.Token, nil)
		require.Equal(t, http.StatusOK, students.Status, "body %v", students.Body)
		require.EqualValues(t, 1, students.data("total"), "the children stay with the class")

		late := registerWaliWith(t, fmt.Sprintf("wali.%s@example.com", suffix()), nisn(), joinCode, "Late Child")
		require.Equal(t, http.StatusCreated, late.Status, "the class takes in new children, body %v", late.Body)
	})
}

func TestARefusedDeletionLeavesTheClassesAsTheyWere(t *testing.T) {
	leaving := registerGuru(t, "Dahlia Sendiri")
	partner := registerGuru(t, "Edelweis")

	// the second class is shared and owned by the account that is leaving, the first has nobody else
	sharedID, _ := classOf(t, partner)
	partnerID, _ := ownerOf(t, partner.Token, sharedID)
	added := call(t, http.MethodPost, fmt.Sprintf("/api/v1/guru/classes/%s/teachers", sharedID), partner.Token, map[string]any{"email": leaving.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	leavingID, _ := added.data("id").(string)

	give := call(t, http.MethodPut, fmt.Sprintf("/api/v1/guru/classes/%s/owner", sharedID), partner.Token, map[string]any{"teacherId": leavingID})
	require.Equal(t, http.StatusOK, give.Status, "body %v", give.Body)

	// a class without children would be archived, so a child keeps the first class from being let go
	_, leavingCode := classOf(t, leaving)
	registerWali(t, leavingCode, "Child Of Dahlia")

	deleteAccount(t, leaving.Token, leaving.Password).requireError(t, http.StatusConflict, "SUCCESSOR_REQUIRED")

	who, count := ownerOf(t, partner.Token, sharedID)
	require.Equal(t, leavingID, who, "the hand over of the shared class was rolled back with the refusal")
	require.Equal(t, 2, count)

	// with a successor the shared class goes to the teacher who stays and the class nobody else teaches to the successor
	successor := registerGuru(t, "Flamboyan")
	require.Equal(t, http.StatusOK, deleteAccountTo(t, leaving.Token, leaving.Password, successor.Email).Status)

	who, count = ownerOf(t, partner.Token, sharedID)
	require.Equal(t, partnerID, who, "the teacher who stays owns the shared class")
	require.Equal(t, 1, count)

	list := call(t, http.MethodGet, "/api/v1/guru/classes", successor.Token, nil)
	require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)
	require.Len(t, listOf(t, list, "classes"), 2, "the successor teaches the class of its own and the one it took over")
}

func TestEveryClassOfAnAccountGoesToTheSuccessor(t *testing.T) {
	leaving := registerGuru(t, "Gardenia")
	successor := registerGuru(t, "Heliconia")

	for _, name := range []string{"Iris", "Jasmine"} {
		res := call(t, http.MethodPost, "/api/v1/guru/classes", leaving.Token, map[string]any{"name": name})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
	}

	before := call(t, http.MethodGet, "/api/v1/guru/classes", leaving.Token, nil)
	require.Len(t, listOf(t, before, "classes"), 3)

	require.Equal(t, http.StatusOK, deleteAccountTo(t, leaving.Token, leaving.Password, successor.Email).Status)

	after := call(t, http.MethodGet, "/api/v1/guru/classes", successor.Token, nil)
	require.Equal(t, http.StatusOK, after.Status, "body %v", after.Body)

	classes := listOf(t, after, "classes")
	require.Len(t, classes, 4, "three taken over and the one of its own")

	for _, item := range classes {
		id, _ := dig(item, "id").(string)
		who, count := ownerOf(t, successor.Token, id)
		require.NotEmpty(t, who, "class %s has an owner", id)
		require.Equal(t, 1, count)
	}
}

// Two teachers who each name the other as successor and delete at the same moment must not leave a class behind. The one
// that is served second finds the successor gone and is refused, because it would be the only teacher of both classes.
func TestTwoTeachersNamingEachOtherNeverOrphanAClass(t *testing.T) {
	for round := range 4 {
		first := registerGuru(t, fmt.Sprintf("Kenanga %d", round))
		second := registerGuru(t, fmt.Sprintf("Lili %d", round))
		firstClass, _ := classOf(t, first)
		secondClass, _ := classOf(t, second)

		var wg sync.WaitGroup

		var firstResult, secondResult result

		wg.Add(2)

		go func() {
			defer wg.Done()

			firstResult = deleteAccountTo(t, first.Token, first.Password, second.Email)
		}()

		go func() {
			defer wg.Done()

			secondResult = deleteAccountTo(t, second.Token, second.Password, first.Email)
		}()

		wg.Wait()

		require.ElementsMatch(t, []int{http.StatusOK, http.StatusNotFound}, []int{firstResult.Status, secondResult.Status},
			"round %d, bodies %v and %v", round, firstResult.Body, secondResult.Body)

		// the account that was served first is gone, so the other one is the survivor
		survivor := second
		if firstResult.Status != http.StatusOK {
			survivor = first
		}

		for _, classID := range []string{firstClass, secondClass} {
			who, count := ownerOf(t, survivor.Token, classID)
			require.NotEmpty(t, who, "round %d, class %s keeps an owner", round, classID)
			require.Equal(t, 1, count)
		}
	}
}
