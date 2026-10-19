package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// codeOf reads the join code of the class as one of its teachers sees it
func codeOf(t *testing.T, token string, classID string) string {
	t.Helper()

	res := call(t, http.MethodGet, "/api/v1/guru/classes/"+classID, token, nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	code, _ := res.data("joinCode").(string)
	require.Len(t, code, 8)

	return code
}

func TestTheJoinCodeIsReplacedWhenATeacherLeavesOrIsRemoved(t *testing.T) {
	owner := registerGuru(t, "Akasia")
	second := registerGuru(t, "Bakung")
	third := registerGuru(t, "Cemara")
	classID, firstCode := classOf(t, owner)

	base := "/api/v1/guru/classes/" + classID
	add := func(email string) string {
		res := call(t, http.MethodPost, base+"/teachers", owner.Token, map[string]any{"email": email})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		id, _ := res.data("id").(string)

		return id
	}

	joined, _ := registerWali(t, firstCode, "Joined Before")
	secondID := add(second.Email)
	thirdID := add(third.Email)

	t.Run("adding a teacher keeps the code", func(t *testing.T) {
		require.Equal(t, firstCode, codeOf(t, owner.Token, classID))
	})

	t.Run("a refused removal keeps the code", func(t *testing.T) {
		call(t, http.MethodDelete, base+"/teachers/"+thirdID, second.Token, nil).requireError(t, http.StatusForbidden, "CLASS_OWNER_REQUIRED")
		require.Equal(t, firstCode, codeOf(t, owner.Token, classID))
	})

	t.Run("removing a teacher replaces the code", func(t *testing.T) {
		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, base+"/teachers/"+thirdID, owner.Token, nil).Status)

		now := codeOf(t, owner.Token, classID)
		require.NotEqual(t, firstCode, now)
		require.Equal(t, now, codeOf(t, second.Token, classID), "the teachers who stay see the same new code")

		registerWaliWith(t, fmt.Sprintf("old.%s@example.com", suffix()), nisn(), firstCode, "Old Code Child").
			requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")

		res := registerWaliWith(t, fmt.Sprintf("new.%s@example.com", suffix()), nisn(), now, "New Code Child")
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		firstCode = now
	})

	t.Run("a teacher who leaves replaces the code too", func(t *testing.T) {
		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, base+"/teachers/"+secondID, second.Token, nil).Status)
		require.NotEqual(t, firstCode, codeOf(t, owner.Token, classID))
	})

	t.Run("a parent who joined before keeps the class", func(t *testing.T) {
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/wali/dashboard", joined.Token, nil).Status)
	})
}
