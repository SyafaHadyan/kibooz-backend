package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// verifyNISN has a teacher of the class vouch for the NISN of a child, and checks that it worked
func verifyNISN(t *testing.T, guru account, classID string, studentID string) result {
	t.Helper()

	res := call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/students/"+studentID+"/nisn-verification", guru.Token, nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	return res
}

// registeredChild registers a parent with a child that has the NISN and returns the token of the parent and the id of the child
func registeredChild(t *testing.T, joinCode string, number string, childName string) (account, string) {
	t.Helper()

	email := fmt.Sprintf("nisn.%s@example.com", suffix())
	res := registerWaliWith(t, email, number, joinCode, childName)
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

	parent := account{Token: res.data("token").(string), Email: email, Password: testPassword}
	dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", parent.Token, nil)
	require.Equal(t, http.StatusOK, dashboard.Status, "body %v", dashboard.Body)

	return parent, dashboard.data("student", "id").(string)
}

func TestATeacherVerifiesANISN(t *testing.T) {
	guruA := registerGuru(t, "Verbena")
	guruB := registerGuru(t, "Wisteria")
	outsider := registerGuru(t, "Xeranthemum")
	classA, codeA := classOf(t, guruA)
	classB, codeB := classOf(t, guruB)

	shared := nisn()

	// nothing is verified yet, so anybody can register the same number, in this class or in another one
	parentA, childA := registeredChild(t, codeA, shared, "Anak Verified")
	parentB, childB := registeredChild(t, codeB, shared, "Anak Squatter B")
	parentC, childC := registeredChild(t, codeA, shared, "Anak Squatter C")

	child := func(parent account, id string) result {
		res := call(t, http.MethodGet, "/api/v1/wali/child/"+id, parent.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		return res
	}

	verifyPath := func(classID string, studentID string) string {
		return "/api/v1/guru/classes/" + classID + "/students/" + studentID + "/nisn-verification"
	}

	t.Run("a new child starts unverified with the number it was registered with", func(t *testing.T) {
		for _, c := range []struct {
			parent account
			id     string
		}{{parentA, childA}, {parentB, childB}, {parentC, childC}} {
			res := child(c.parent, c.id)
			require.Equal(t, shared, res.data("nisn"))
			require.Equal(t, false, res.data("nisnVerified"))
			require.Equal(t, false, res.data("nisnNeedsUpdate"))
		}
	})

	t.Run("people who may not verify are refused and nothing changes", func(t *testing.T) {
		call(t, http.MethodPost, verifyPath(classA, childA), outsider.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodPost, verifyPath(classA, childA), parentA.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, verifyPath(classA, childA), "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodPost, verifyPath(classA, uuid.NewString()), guruA.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodPost, verifyPath(classA, childB), guruA.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodPost, verifyPath(uuid.NewString(), childA), guruA.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodPost, verifyPath("not-a-uuid", childA), guruA.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, verifyPath(classA, "not-a-uuid"), guruA.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		require.Equal(t, false, child(parentA, childA).data("nisnVerified"))
		require.Equal(t, shared, child(parentB, childB).data("nisn"))
	})

	t.Run("the verified child keeps the number and every other holder loses it, in any class", func(t *testing.T) {
		res := verifyNISN(t, guruA, classA, childA)
		require.Equal(t, true, res.data("nisnVerified"))
		require.EqualValues(t, 2, res.data("displaced"))

		verified := child(parentA, childA)
		require.Equal(t, shared, verified.data("nisn"))
		require.Equal(t, true, verified.data("nisnVerified"))
		require.Equal(t, false, verified.data("nisnNeedsUpdate"))

		for _, c := range []struct {
			parent account
			id     string
		}{{parentB, childB}, {parentC, childC}} {
			lost := child(c.parent, c.id)
			require.Equal(t, "", lost.data("nisn"), "the child keeps its record with an empty number")
			require.Equal(t, false, lost.data("nisnVerified"))
			require.Equal(t, true, lost.data("nisnNeedsUpdate"))
		}

		list := call(t, http.MethodGet, "/api/v1/wali/children", parentB.Token, nil)
		require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)
		require.Equal(t, true, dig(listOf(t, list, "children")[0], "nisnNeedsUpdate"))

		roster := call(t, http.MethodGet, "/api/v1/classes/"+classB+"/students", guruB.Token, nil)
		require.Equal(t, http.StatusOK, roster.Status, "body %v", roster.Body)
		require.Equal(t, "", dig(listOf(t, roster, "students")[0], "nisn"))
		require.Equal(t, true, dig(listOf(t, roster, "students")[0], "nisnNeedsUpdate"))

		roster = call(t, http.MethodGet, "/api/v1/classes/"+classA+"/students", guruA.Token, nil)
		require.Equal(t, http.StatusOK, roster.Status, "body %v", roster.Body)
		require.Len(t, listOf(t, roster, "students"), 2)
	})

	t.Run("verifying again changes nothing", func(t *testing.T) {
		res := verifyNISN(t, guruA, classA, childA)
		require.EqualValues(t, 0, res.data("displaced"))
		require.Equal(t, shared, child(parentA, childA).data("nisn"))
	})

	t.Run("a child without a number cannot be verified", func(t *testing.T) {
		call(t, http.MethodPost, verifyPath(classB, childB), guruB.Token, nil).requireError(t, http.StatusConflict, "NISN_NOT_SET")
	})

	t.Run("a verified number is taken, whoever asks for it", func(t *testing.T) {
		patch := call(t, http.MethodPatch, "/api/v1/wali/children/"+childB, parentB.Token, map[string]any{"nisn": shared})
		patch.requireError(t, http.StatusConflict, "NISN_ALREADY_REGISTERED")

		add := call(t, http.MethodPost, "/api/v1/wali/children", parentB.Token, map[string]any{
			"classCode": codeB, "student": map[string]any{"nisn": shared, "fullName": "Another Child"},
		})
		add.requireError(t, http.StatusConflict, "NISN_ALREADY_REGISTERED")

		registerWaliWith(t, fmt.Sprintf("late.%s@example.com", suffix()), shared, codeB, "Late Child").
			requireError(t, http.StatusConflict, "NISN_ALREADY_REGISTERED")
	})

	t.Run("a child that lost the number enters the right one and stays unverified", func(t *testing.T) {
		right := nisn()

		res := call(t, http.MethodPatch, "/api/v1/wali/children/"+childB, parentB.Token, map[string]any{"nisn": right})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, right, res.data("nisn"))
		require.Equal(t, false, res.data("nisnVerified"))
		require.Equal(t, false, res.data("nisnNeedsUpdate"))
	})

	t.Run("a new number takes the verification off and the same number again does not", func(t *testing.T) {
		same := call(t, http.MethodPatch, "/api/v1/wali/children/"+childA, parentA.Token, map[string]any{"nisn": shared, "fullName": "Anak Verified Baru"})
		require.Equal(t, http.StatusOK, same.Status, "body %v", same.Body)
		require.Equal(t, true, child(parentA, childA).data("nisnVerified"), "naming the same number is no change")

		changed := nisn()
		res := call(t, http.MethodPatch, "/api/v1/wali/children/"+childA, parentA.Token, map[string]any{"nisn": changed})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, false, res.data("nisnVerified"), "the teacher vouched for the old number only")
		require.Equal(t, changed, child(parentA, childA).data("nisn"))

		// the old number is free again, because nobody holds it as a verified one
		reused := registerWaliWith(t, fmt.Sprintf("free.%s@example.com", suffix()), shared, codeB, "Takes The Old Number")
		require.Equal(t, http.StatusCreated, reused.Status, "body %v", reused.Body)
	})

	t.Run("a teacher takes a wrong verification back", func(t *testing.T) {
		verifyNISN(t, guruB, classB, childB)

		res := call(t, http.MethodDelete, verifyPath(classB, childB), guruB.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, false, res.data("nisnVerified"))
		require.Equal(t, false, child(parentB, childB).data("nisnVerified"))

		again := call(t, http.MethodDelete, verifyPath(classB, childB), guruB.Token, nil)
		require.Equal(t, http.StatusOK, again.Status, "taking back what is not there changes nothing, body %v", again.Body)
		require.Equal(t, false, again.data("nisnVerified"))

		call(t, http.MethodDelete, verifyPath(classB, childB), outsider.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodDelete, verifyPath(classB, childB), parentB.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodDelete, verifyPath(classB, childA), guruB.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})
}

// A verification and a registration, an added child or a parent's own change of the number all take the lock of the class and the
// lock of the NISN. They have to take them in the same order, or a pair of them waits for each other until the database gives one up
// with a deadlock error, which the caller sees as a 500.
func TestAVerificationAndARegistrationOfTheSameNISNDoNotDeadlock(t *testing.T) {
	guru := registerGuru(t, "Yasmin")
	classID, code := classOf(t, guru)

	for round := 0; round < 6; round++ {
		number := nisn()
		_, held := registeredChild(t, code, number, fmt.Sprintf("Anak Pemegang %d", round))
		other, otherChild := registeredChild(t, code, nisn(), fmt.Sprintf("Anak Pengubah %d", round))

		var wg sync.WaitGroup

		answers := make([]result, 3)

		wg.Add(3)

		go func() {
			defer wg.Done()

			answers[0] = call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/students/"+held+"/nisn-verification", guru.Token, nil)
		}()

		go func() {
			defer wg.Done()

			answers[1] = registerWaliWith(t, fmt.Sprintf("race.%s@example.com", suffix()), number, code, "Anak Baru")
		}()

		go func() {
			defer wg.Done()

			answers[2] = call(t, http.MethodPatch, "/api/v1/wali/children/"+otherChild, other.Token, map[string]any{"nisn": number})
		}()

		wg.Wait()

		require.Equal(t, http.StatusOK, answers[0].Status, "round %d, body %v", round, answers[0].Body)

		for _, res := range answers[1:] {
			require.Contains(t, []int{http.StatusCreated, http.StatusOK, http.StatusConflict}, res.Status, "round %d, body %v", round, res.Body)
		}
	}
}

// Two teachers verify the same number for two children in two classes at once. The number may belong to one verified child only.
func TestTwoVerificationsOfTheSameNISNLeaveOneVerifiedChild(t *testing.T) {
	guruA := registerGuru(t, "Zinnia")
	guruB := registerGuru(t, "Amaryllis")
	classA, codeA := classOf(t, guruA)
	classB, codeB := classOf(t, guruB)

	for round := 0; round < 6; round++ {
		number := nisn()
		_, childA := registeredChild(t, codeA, number, fmt.Sprintf("Anak A %d", round))
		_, childB := registeredChild(t, codeB, number, fmt.Sprintf("Anak B %d", round))

		var wg sync.WaitGroup

		answers := make([]result, 2)

		wg.Add(2)

		go func() {
			defer wg.Done()

			answers[0] = call(t, http.MethodPost, "/api/v1/guru/classes/"+classA+"/students/"+childA+"/nisn-verification", guruA.Token, nil)
		}()

		go func() {
			defer wg.Done()

			answers[1] = call(t, http.MethodPost, "/api/v1/guru/classes/"+classB+"/students/"+childB+"/nisn-verification", guruB.Token, nil)
		}()

		wg.Wait()

		wins := 0

		for _, res := range answers {
			if res.Status == http.StatusOK {
				wins++

				continue
			}

			require.Equal(t, http.StatusConflict, res.Status, "round %d, body %v", round, res.Body)
			require.Contains(t, []any{"NISN_NOT_SET", "NISN_ALREADY_REGISTERED"}, res.Body["errorCode"])
		}

		require.Equal(t, 1, wins, "round %d, one teacher gets the number and the other one is told that it is gone", round)

		var verified int64

		require.NoError(t, testDB.Raw("SELECT count(*) FROM students WHERE nisn = ? AND nisn_verified_at IS NOT NULL AND deleted_at IS NULL", number).
			Scan(&verified).Error)
		require.EqualValues(t, 1, verified)
	}
}

// The teacher who vouched for a NISN is stored, so a verification that took a number from a real child can be traced. It is never
// shown to a parent, which is why the checks read the table.
func TestTheTeacherWhoVerifiedANISNIsRecorded(t *testing.T) {
	guru := registerGuru(t, "Zephyranthes")
	colleague := registerGuru(t, "Yucca")
	classID, code := classOf(t, guru)

	require.Equal(t, http.StatusCreated, call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/teachers", guru.Token, map[string]any{"email": colleague.Email}).Status)

	parent, childID := registeredChild(t, code, nisn(), "Anak Tercatat")

	teacherID := func(who account) string {
		var id string

		require.NoError(t, testDB.Raw("SELECT g.id::text FROM gurus g JOIN users u ON u.id = g.user_id WHERE u.email = ?", who.Email).Scan(&id).Error)
		require.NotEmpty(t, id)

		return id
	}

	recorded := func() string {
		var by string

		require.NoError(t, testDB.Raw("SELECT COALESCE(nisn_verified_by::text, '') FROM students WHERE id = ?::uuid", childID).Scan(&by).Error)

		return by
	}

	require.Empty(t, recorded(), "nobody vouched yet")

	verifyNISN(t, guru, classID, childID)
	require.Equal(t, teacherID(guru), recorded())

	verifyNISN(t, colleague, classID, childID)
	require.Equal(t, teacherID(guru), recorded(), "a second verification keeps the first teacher")

	res := call(t, http.MethodDelete, "/api/v1/guru/classes/"+classID+"/students/"+childID+"/nisn-verification", colleague.Token, nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	require.Empty(t, recorded(), "taking the verification back clears the teacher")

	verifyNISN(t, colleague, classID, childID)
	require.Equal(t, teacherID(colleague), recorded())

	patch := call(t, http.MethodPatch, "/api/v1/wali/children/"+childID, parent.Token, map[string]any{"nisn": nisn()})
	require.Equal(t, http.StatusOK, patch.Status, "body %v", patch.Body)
	require.Empty(t, recorded(), "a new number from the parent clears the teacher with the verification")

	// a row that has a time and no teacher, as one that predates the column would, keeps no teacher when another one verifies it
	require.NoError(t, testDB.Exec("UPDATE students SET nisn_verified_at = now(), nisn_verified_by = NULL WHERE id = ?::uuid", childID).Error)
	verifyNISN(t, guru, classID, childID)
	require.Empty(t, recorded(), "the first verification is unknown, and a later click does not claim it")

	verifyNISN(t, colleague, classID, childID)
	require.NoError(t, testDB.Exec("UPDATE students SET nisn_verified_at = NULL, nisn_verified_by = NULL WHERE id = ?::uuid", childID).Error)

	// nothing that the API answers holds the id of the teacher
	for name, res := range map[string]result{
		"the child of the parent":    call(t, http.MethodGet, "/api/v1/wali/child/"+childID, parent.Token, nil),
		"the children of the parent": call(t, http.MethodGet, "/api/v1/wali/children", parent.Token, nil),
		"the roster":                 call(t, http.MethodGet, "/api/v1/classes/"+classID+"/students", guru.Token, nil),
		"a verification":             call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/students/"+childID+"/nisn-verification", guru.Token, nil),
	} {
		require.Equal(t, http.StatusOK, res.Status, "%s, body %v", name, res.Body)
		require.NotContains(t, fmt.Sprint(res.Body), teacherID(guru), name)
		require.NotContains(t, fmt.Sprint(res.Body), teacherID(colleague), name)
	}
}
