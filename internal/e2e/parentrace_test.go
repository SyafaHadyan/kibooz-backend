package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func addChildTo(t *testing.T, token string, classCode string, name string) result {
	t.Helper()

	return call(t, http.MethodPost, "/api/v1/wali/children", token, map[string]any{
		"classCode": classCode, "student": map[string]any{"nisn": nisn(), "fullName": name},
	})
}

func activeChildrenOf(t *testing.T, waliID string) int64 {
	t.Helper()

	var active int64

	require.NoError(t, testDB.Raw("SELECT count(*) FROM students WHERE wali_id = ?::uuid AND deleted_at IS NULL", waliID).Scan(&active).Error)

	return active
}

func waliIDOfEmail(t *testing.T, email string) string {
	t.Helper()

	var waliID string

	require.NoError(t, testDB.Raw(
		"SELECT w.id::text FROM walis w JOIN users u ON u.id = w.user_id WHERE u.email = ?", email,
	).Scan(&waliID).Error)
	require.NotEmpty(t, waliID)

	return waliID
}

// Adding a child counts the children of the parent under a lock on the parent row. A deletion that commits while the request
// waits for that lock leaves nothing to lock, and the child must not be created for a parent who is gone.
func TestAChildIsNotAddedToAParentWhoseAccountWasDeletedWhileTheRequestWaited(t *testing.T) {
	guru := registerGuru(t, "Anak Menunggu")
	_, joinCode := classOf(t, guru)
	parent, _ := registerWali(t, joinCode, "Anak Pertama")
	waliID := waliIDOfEmail(t, parent.Email)

	deleting := testDB.Begin()
	require.NoError(t, deleting.Error)

	t.Cleanup(func() { _ = deleting.Rollback().Error })

	require.NoError(t, deleting.Exec("UPDATE walis SET deleted_at = now() WHERE id = ?::uuid", waliID).Error)

	done := make(chan result, 1)

	go func() { done <- addChildTo(t, parent.Token, joinCode, "Anak Kedua") }()

	waitForLockWaiter(t, "%FROM walis WHERE id%FOR UPDATE%", "the request has to wait for the deletion, or the test proves nothing")

	require.NoError(t, deleting.Commit().Error)

	select {
	case res := <-done:
		res.requireError(t, http.StatusNotFound, "PROFILE_NOT_FOUND")
	case <-time.After(10 * time.Second):
		t.Fatal("the request did not finish after the deletion committed")
	}

	require.EqualValues(t, 1, activeChildrenOf(t, waliID), "only the child of the registration is there, the refused one was not created")
}

// A deletion takes the parent row first and the class locks after it, which is the order that adding a child uses. The other
// way round one of them waits for the other and PostgreSQL has to abort one, and the caller gets a 500.
func TestDeletingAParentWhileAddingAChildNeverDeadlocksAndLeavesNoChild(t *testing.T) {
	first := registerGuru(t, "Kelas Pertama")
	_, firstCode := classOf(t, first)
	second := registerGuru(t, "Kelas Kedua")
	_, secondCode := classOf(t, second)

	for round := range 6 {
		parent, _ := registerWali(t, firstCode, fmt.Sprintf("Anak Ronde %d", round))
		waliID := waliIDOfEmail(t, parent.Email)

		var (
			wg      sync.WaitGroup
			added   result
			removed result
		)

		wg.Add(2)

		go func() {
			defer wg.Done()

			added = addChildTo(t, parent.Token, secondCode, fmt.Sprintf("Anak Tambahan %d", round))
		}()

		go func() {
			defer wg.Done()

			removed = deleteAccount(t, parent.Token, parent.Password)
		}()

		wg.Wait()

		require.Equal(t, http.StatusOK, removed.Status, "round %d, body %v", round, removed.Body)
		require.NotEqual(t, http.StatusInternalServerError, added.Status, "round %d, body %v", round, added.Body)
		require.Zero(t, activeChildrenOf(t, waliID), "round %d, a child of a deleted parent is left behind", round)
	}
}
