package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/idempotency"
)

const chatBase = "/api/v1/chat"

// chatContactOf returns the contact of a list that has the named child, or the only contact when child is empty
func chatContactOf(t *testing.T, res result, child string) map[string]any {
	t.Helper()

	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	list, _ := res.data("contacts").([]any)
	if child == "" {
		require.Len(t, list, 1, "body %v", res.Body)

		return list[0].(map[string]any)
	}

	for _, item := range list {
		contact := item.(map[string]any)

		for _, name := range contact["children"].([]any) {
			if name == child {
				return contact
			}
		}
	}

	require.Failf(t, "no such contact", "no contact with the child %s in %v", child, res.Body)

	return nil
}

func messageIDOf(t *testing.T, res result) int64 {
	t.Helper()

	require.Less(t, res.Status, 300, "body %v", res.Body)

	id, ok := res.data("id").(float64)
	require.True(t, ok, "body %v", res.Body)

	return int64(id)
}

func idsOf(t *testing.T, res result) []int64 {
	t.Helper()

	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	items, _ := res.data("messages").([]any)
	ids := make([]int64, 0, len(items))

	for _, item := range items {
		ids = append(ids, int64(item.(map[string]any)["id"].(float64)))
	}

	return ids
}

func TestTeacherAndParentChat(t *testing.T) {
	guru := registerGuru(t, "Anggrek")
	classID, code := classOf(t, guru)
	parent, raniID := registerWali(t, code, "Rani Putri")
	other, _ := registerWali(t, code, "Dimas Aji")
	stranger := registerGuru(t, "Melati")
	_, strangerCode := classOf(t, stranger)
	farParent, _ := registerWali(t, strangerCode, "Far Child")

	var (
		guruID, parentID, farParentID, strangerID string
		conversationID                            string
		sent                                      []int64
	)

	send := func(token string, conversation string, body string) result {
		res, _ := callKey(t, http.MethodPost, chatBase+"/conversations/"+conversation+"/messages", token, uuid.NewString(), map[string]any{"body": body})

		return res
	}
	start := func(token string, userID string) result {
		return call(t, http.MethodPost, chatBase+"/conversations", token, map[string]any{"userId": userID})
	}
	messages := func(token string, query string) result {
		return call(t, http.MethodGet, chatBase+"/conversations/"+conversationID+"/messages"+query, token, nil)
	}

	t.Run("contacts follow the classes of each account", func(t *testing.T) {
		teacherView := call(t, http.MethodGet, chatBase+"/contacts", guru.Token, nil)
		require.EqualValues(t, 2, teacherView.data("total"), "the two parents of the class and nobody else")

		contact := chatContactOf(t, teacherView, "Rani Putri")
		parentID = contact["id"].(string)

		require.Equal(t, "WALI", contact["role"])
		require.Nil(t, contact["conversationId"], "no conversation exists yet")
		require.Len(t, contact["classes"], 1)
		require.Equal(t, []any{"Rani Putri"}, contact["children"])

		parentView := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", parent.Token, nil), "")
		guruID = parentView["id"].(string)

		require.Equal(t, "GURU", parentView["role"])
		require.Equal(t, []any{"Rani Putri"}, parentView["children"], "a parent sees their own child next to the teacher")

		// the parent of the other class has the other teacher as the only contact, and the other way round
		strangerID = chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", farParent.Token, nil), "")["id"].(string)
		farParentID = chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", stranger.Token, nil), "")["id"].(string)

		page := call(t, http.MethodGet, chatBase+"/contacts?limit=1&page=2", guru.Token, nil)
		require.Equal(t, http.StatusOK, page.Status, "body %v", page.Body)
		require.Len(t, page.data("contacts"), 1)
		require.EqualValues(t, 2, page.data("total"))

		call(t, http.MethodGet, chatBase+"/contacts?limit=0", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, chatBase+"/contacts", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	t.Run("a conversation is one for the pair and starts empty", func(t *testing.T) {
		first := start(parent.Token, guruID)
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)

		conversationID = first.data("id").(string)

		require.Equal(t, guruID, first.data("peer", "id"))
		require.Equal(t, "GURU", first.data("peer", "role"))
		require.Equal(t, true, first.data("canSend"))
		require.Nil(t, first.data("lastMessage"))
		require.EqualValues(t, 0, first.data("unreadCount"))

		again := start(guru.Token, parentID)
		require.Equal(t, http.StatusOK, again.Status, "body %v", again.Body)
		require.Equal(t, conversationID, again.data("id"), "the teacher reaches the same conversation from the other side")
		require.Equal(t, parentID, again.data("peer", "id"))

		require.EqualValues(t, 0, call(t, http.MethodGet, chatBase+"/conversations", guru.Token, nil).data("total"), "a conversation without a message is not listed")

		listed := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", guru.Token, nil), "Rani Putri")
		require.Equal(t, conversationID, listed["conversationId"])
	})

	t.Run("only a teacher and a parent who share a class can start one", func(t *testing.T) {
		start(guru.Token, farParentID).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
		start(parent.Token, strangerID).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
		start(guru.Token, stranger.Email).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		start(guru.Token, uuid.NewString()).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
		start(guru.Token, guruID).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, chatBase+"/conversations", guru.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		// two parents of one class never talk in private, and neither do two teachers
		otherParentID := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", guru.Token, nil), "Dimas Aji")["id"].(string)
		start(parent.Token, otherParentID).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
		start(guru.Token, strangerID).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
	})

	t.Run("sending trims the text and repeats safely", func(t *testing.T) {
		key := uuid.NewString()
		path := chatBase + "/conversations/" + conversationID + "/messages"

		first, header := callKey(t, http.MethodPost, path, parent.Token, key, map[string]any{"body": "  Good morning, Ma'am  "})
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)
		require.Empty(t, header.Get(idempotency.ReplayedHeader))
		require.Equal(t, "Good morning, Ma'am", first.data("body"))
		require.Equal(t, parentID, first.data("senderId"))
		require.Equal(t, false, first.data("deleted"))

		again, againHeader := callKey(t, http.MethodPost, path, parent.Token, key, map[string]any{"body": "  Good morning, Ma'am  "})
		require.Equal(t, "true", againHeader.Get(idempotency.ReplayedHeader))
		require.Equal(t, first.Body, again.Body)

		sent = append(sent, messageIDOf(t, first))

		reply := send(guru.Token, conversationID, "Good morning!")
		require.Equal(t, http.StatusCreated, reply.Status, "body %v", reply.Body)

		sent = append(sent, messageIDOf(t, reply))

		for _, body := range []string{"Rani has a cough today", "She may go home early"} {
			res := send(parent.Token, conversationID, body)
			require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

			sent = append(sent, messageIDOf(t, res))
		}

		require.Len(t, sent, 4)
		require.Greater(t, sent[1], sent[0])
		require.Greater(t, sent[3], sent[2])

		send(parent.Token, conversationID, " \n\t ").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		send(parent.Token, conversationID, strings.Repeat("a", 4001)).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, path, parent.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		require.Equal(t, http.StatusCreated, send(parent.Token, conversationID, strings.Repeat("a", 4000)).Status, "4000 characters fit")
	})

	t.Run("nobody outside the conversation can reach it", func(t *testing.T) {
		// another parent of the same class and a teacher of another class
		for _, token := range []string{other.Token, stranger.Token} {
			messages(token, "").requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
			send(token, conversationID, "hello").requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
			call(t, http.MethodPut, chatBase+"/conversations/"+conversationID+"/read", token, map[string]any{"messageId": 0}).
				requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
			call(t, http.MethodDelete, chatBase+"/conversations/"+conversationID+"/messages/"+"1", token, nil).
				requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
		}

		send(guru.Token, uuid.NewString(), "hello").requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
		call(t, http.MethodGet, chatBase+"/conversations/not-a-uuid/messages", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a conversation lists the last message and the unread count of each side", func(t *testing.T) {
		list := call(t, http.MethodGet, chatBase+"/conversations", guru.Token, nil)
		require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)
		require.EqualValues(t, 1, list.data("total"))

		item := list.data("conversations").([]any)[0].(map[string]any)
		require.Equal(t, conversationID, item["id"])
		require.EqualValues(t, 4, item["unreadCount"], "three short messages and the long one of the parent")
		require.EqualValues(t, 0, item["peerLastReadMessageId"])

		last := item["lastMessage"].(map[string]any)
		require.Equal(t, strings.Repeat("a", 4000), last["body"])

		parentList := call(t, http.MethodGet, chatBase+"/conversations", parent.Token, nil)
		require.EqualValues(t, 1, parentList.data("conversations").([]any)[0].(map[string]any)["unreadCount"], "the one answer of the teacher")
	})

	t.Run("a window of messages is read forwards and backwards", func(t *testing.T) {
		all := idsOf(t, messages(guru.Token, "?limit=100"))
		require.Len(t, all, 5)
		require.Equal(t, sent, all[:4])

		latest := messages(guru.Token, "?limit=2")
		require.Equal(t, all[3:], idsOf(t, latest), "the latest two, still oldest first")
		require.Equal(t, true, latest.data("hasMore"))

		older := messages(guru.Token, "?limit=10&before="+itoa(all[3]))
		require.Equal(t, all[:3], idsOf(t, older))
		require.Equal(t, false, older.data("hasMore"))

		forward := messages(guru.Token, "?limit=3&after=0")
		require.Equal(t, all[:3], idsOf(t, forward))
		require.Equal(t, true, forward.data("hasMore"))

		caughtUp := messages(guru.Token, "?after="+itoa(all[4]))
		require.Empty(t, idsOf(t, caughtUp))
		require.Equal(t, false, caughtUp.data("hasMore"))

		messages(guru.Token, "?after=1&before=5").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		messages(guru.Token, "?limit=0").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		messages(guru.Token, "?limit=101").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		messages(guru.Token, "?after=abc").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		messages(guru.Token, "?after=-1").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		messages(guru.Token, "?before=0").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a read mark only moves forward and never past the newest message", func(t *testing.T) {
		read := func(token string, id any) result {
			return call(t, http.MethodPut, chatBase+"/conversations/"+conversationID+"/read", token, map[string]any{"messageId": id})
		}

		res := read(guru.Token, sent[2])
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, sent[2], res.data("lastReadMessageId"))
		require.EqualValues(t, 2, res.data("unreadCount"), "the last two messages of the parent are still unread")

		back := read(guru.Token, sent[0])
		require.EqualValues(t, sent[2], back.data("lastReadMessageId"), "the mark does not move back")

		past := read(guru.Token, 1_000_000_000)
		require.EqualValues(t, 0, past.data("unreadCount"))
		require.Greater(t, int64(past.data("lastReadMessageId").(float64)), sent[3], "cut to the newest message, which is the long one")

		seen := call(t, http.MethodGet, chatBase+"/conversations", parent.Token, nil).data("conversations").([]any)[0].(map[string]any)
		require.Equal(t, past.data("lastReadMessageId"), seen["peerLastReadMessageId"], "the parent sees how far the teacher has read")

		read(guru.Token, -1).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPut, chatBase+"/conversations/"+conversationID+"/read", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("only the sender deletes a message and a placeholder stays", func(t *testing.T) {
		path := chatBase + "/conversations/" + conversationID + "/messages/" + itoa(sent[1])

		call(t, http.MethodDelete, path, parent.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		res := call(t, http.MethodDelete, path, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, true, res.data("deleted"))
		require.Equal(t, "", res.data("body"))

		again := call(t, http.MethodDelete, path, guru.Token, nil)
		require.Equal(t, http.StatusOK, again.Status, "body %v", again.Body)
		require.Equal(t, res.Body, again.Body, "deleting again changes nothing")

		window := messages(parent.Token, "?limit=100").data("messages").([]any)
		require.Equal(t, true, window[1].(map[string]any)["deleted"])
		require.Equal(t, "", window[1].(map[string]any)["body"])

		call(t, http.MethodDelete, chatBase+"/conversations/"+conversationID+"/messages/999999999", guru.Token, nil).
			requireError(t, http.StatusNotFound, "CHAT_MESSAGE_NOT_FOUND")
		call(t, http.MethodDelete, chatBase+"/conversations/"+conversationID+"/messages/0", guru.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodDelete, chatBase+"/conversations/"+conversationID+"/messages/abc", guru.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("history stays readable when the two stop sharing a class but sending closes", func(t *testing.T) {
		removed := call(t, http.MethodDelete, "/api/v1/guru/classes/"+classID+"/students/"+raniID, guru.Token, nil)
		require.Equal(t, http.StatusOK, removed.Status, "body %v", removed.Body)

		require.EqualValues(t, 0, call(t, http.MethodGet, chatBase+"/contacts", parent.Token, nil).data("total"), "the teacher is no longer a contact")
		require.EqualValues(t, 1, call(t, http.MethodGet, chatBase+"/contacts", guru.Token, nil).data("total"), "only the other parent is left")

		item := call(t, http.MethodGet, chatBase+"/conversations", parent.Token, nil).data("conversations").([]any)[0].(map[string]any)
		require.Equal(t, conversationID, item["id"])
		require.Equal(t, false, item["canSend"])

		send(parent.Token, conversationID, "One more thing").requireError(t, http.StatusConflict, "CHAT_SENDING_CLOSED")
		send(guru.Token, conversationID, "One more thing").requireError(t, http.StatusConflict, "CHAT_SENDING_CLOSED")
		require.Len(t, idsOf(t, messages(parent.Token, "?limit=100")), 5, "reading is still open and the refused messages were not stored")

		reopened := start(parent.Token, guruID)
		require.Equal(t, http.StatusOK, reopened.Status, "body %v", reopened.Body)
		require.Equal(t, conversationID, reopened.data("id"), "the old conversation can still be opened")
		require.Equal(t, false, reopened.data("canSend"))

		fromTeacher := start(guru.Token, parentID)
		require.Equal(t, http.StatusOK, fromTeacher.Status, "body %v", fromTeacher.Body)
		require.Equal(t, conversationID, fromTeacher.data("id"))
		require.Equal(t, false, fromTeacher.data("canSend"))
	})
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestClassRoomChat(t *testing.T) {
	guru := registerGuru(t, "Cempaka")
	classID, code := classOf(t, guru)
	parent, raniID := registerWali(t, code, "Rani Putri")
	other, _ := registerWali(t, code, "Dimas Aji")
	stranger := registerGuru(t, "Dahlia")
	_, strangerCode := classOf(t, stranger)
	farParent, _ := registerWali(t, strangerCode, "Far Child")

	var (
		roomID string
		sent   []int64
	)

	open := func(token string, id string) result {
		return call(t, http.MethodPost, chatBase+"/conversations", token, map[string]any{"classId": id})
	}
	send := func(token string, body string) result {
		res, _ := callKey(t, http.MethodPost, chatBase+"/conversations/"+roomID+"/messages", token, uuid.NewString(), map[string]any{"body": body})

		return res
	}
	messages := func(token string, query string) result {
		return call(t, http.MethodGet, chatBase+"/conversations/"+roomID+"/messages"+query, token, nil)
	}

	t.Run("a member opens the room of the class and it is one for everyone", func(t *testing.T) {
		first := open(guru.Token, classID)
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)

		roomID = first.data("id").(string)

		require.Equal(t, "class", first.data("kind"))
		require.Nil(t, first.data("peer"), "a room has no single other person")
		require.Equal(t, classID, first.data("class", "id"))
		require.Equal(t, true, first.data("canSend"))
		require.Nil(t, first.data("lastMessage"))

		again := open(parent.Token, classID)
		require.Equal(t, http.StatusOK, again.Status, "body %v", again.Body)
		require.Equal(t, roomID, again.data("id"), "a parent reaches the same room")

		require.Equal(t, roomID, open(other.Token, classID).data("id"))
		require.EqualValues(t, 0, call(t, http.MethodGet, chatBase+"/conversations", guru.Token, nil).data("total"), "a room without a message is not listed")
	})

	t.Run("only the members of the class can open it", func(t *testing.T) {
		open(farParent.Token, classID).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
		open(stranger.Token, classID).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
		open(guru.Token, uuid.NewString()).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")

		both := call(t, http.MethodPost, chatBase+"/conversations", guru.Token, map[string]any{"classId": classID, "userId": uuid.NewString()})
		both.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		for _, token := range []string{farParent.Token, stranger.Token} {
			messages(token, "").requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
			send(token, "hello").requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
		}
	})

	t.Run("everyone in the room reads what the others write and sees who wrote it", func(t *testing.T) {
		for _, step := range []struct {
			token, body string
		}{
			{guru.Token, "Welcome to the class room"},
			{parent.Token, "Thank you, Ma'am"},
			{other.Token, "Good morning everyone"},
		} {
			res := send(step.token, step.body)
			require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

			sent = append(sent, messageIDOf(t, res))
		}

		window := messages(parent.Token, "?limit=100")
		require.Equal(t, sent, idsOf(t, window))

		items := window.data("messages").([]any)
		teacherName := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", parent.Token, nil), "")["fullName"]
		require.Equal(t, teacherName, items[0].(map[string]any)["sender"].(map[string]any)["fullName"])
		require.Equal(t, "GURU", items[0].(map[string]any)["sender"].(map[string]any)["role"])
		require.Equal(t, "WALI", items[2].(map[string]any)["sender"].(map[string]any)["role"])

		list := call(t, http.MethodGet, chatBase+"/conversations", parent.Token, nil)
		require.EqualValues(t, 1, list.data("total"))

		item := list.data("conversations").([]any)[0].(map[string]any)
		require.Equal(t, roomID, item["id"])
		require.Equal(t, "class", item["kind"])
		require.EqualValues(t, 2, item["unreadCount"], "the teacher and the other parent wrote, the parent's own message does not count")
		require.EqualValues(t, 0, item["peerLastReadMessageId"], "a room has no single other side to show a read mark for")
		require.Equal(t, "Good morning everyone", item["lastMessage"].(map[string]any)["body"])
	})

	t.Run("a parent who joins later sees only what is sent from then on", func(t *testing.T) {
		late, _ := registerWali(t, code, "Latecomer Kid")

		joined := open(late.Token, classID)
		require.Equal(t, http.StatusOK, joined.Status, "body %v", joined.Body)
		require.Equal(t, roomID, joined.data("id"))
		require.Nil(t, joined.data("lastMessage"), "the earlier messages are out of sight")
		require.Empty(t, idsOf(t, messages(late.Token, "?limit=100")))
		require.EqualValues(t, 0, call(t, http.MethodGet, chatBase+"/conversations", late.Token, nil).data("total"))

		res := send(guru.Token, "A new message after the latecomer joined")
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		fresh := messageIDOf(t, res)
		sent = append(sent, fresh)

		require.Equal(t, []int64{fresh}, idsOf(t, messages(late.Token, "?limit=100")))
		require.Equal(t, sent, idsOf(t, messages(parent.Token, "?limit=100")), "the older members still see everything")

		backwards := messages(late.Token, "?limit=100&before="+itoa(fresh))
		require.Empty(t, idsOf(t, backwards), "paging back never reaches the older messages")

		read := call(t, http.MethodPut, chatBase+"/conversations/"+roomID+"/read", late.Token, map[string]any{"messageId": 1_000_000_000})
		require.Equal(t, http.StatusOK, read.Status, "body %v", read.Body)
		require.EqualValues(t, fresh, read.data("lastReadMessageId"), "the mark stops at the newest message the member can see")
		require.EqualValues(t, 0, read.data("unreadCount"))
	})

	t.Run("only the author deletes a message of the room", func(t *testing.T) {
		path := chatBase + "/conversations/" + roomID + "/messages/" + itoa(sent[1])

		call(t, http.MethodDelete, path, guru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		res := call(t, http.MethodDelete, path, parent.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, true, res.data("deleted"))
		require.Equal(t, "", res.data("body"))
	})

	t.Run("a parent loses the room when the child leaves the class", func(t *testing.T) {
		removed := call(t, http.MethodDelete, "/api/v1/guru/classes/"+classID+"/students/"+raniID, guru.Token, nil)
		require.Equal(t, http.StatusOK, removed.Status, "body %v", removed.Body)

		messages(parent.Token, "").requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
		send(parent.Token, "still here?").requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
		open(parent.Token, classID).requireError(t, http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED")
		require.EqualValues(t, 0, call(t, http.MethodGet, chatBase+"/conversations", parent.Token, nil).data("total"))

		require.Equal(t, http.StatusOK, messages(guru.Token, "").Status, "the teacher and the other parent keep the room")
	})
}

// paddedPNG is the tiny test image with a text chunk of the given size after its header. The server cuts the chunk out, but the
// upload is that large when it arrives, which is what a real photo is.
func paddedPNG(t *testing.T, png []byte, size int) []byte {
	t.Helper()

	// signature (8) and the IHDR chunk (4 length, 4 type, 13 data, 4 crc) come first
	const headerEnd = 8 + 4 + 4 + 13 + 4

	chunk := make([]byte, 0, size+12)
	body := append([]byte("Comment\x00"), bytes.Repeat([]byte("x"), size)...)

	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(body)))

	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(append([]byte("tEXt"), body...)))

	chunk = append(chunk, length...)
	chunk = append(chunk, "tEXt"...)
	chunk = append(chunk, body...)
	chunk = append(chunk, crc...)

	out := append([]byte(nil), png[:headerEnd]...)
	out = append(out, chunk...)

	return append(out, png[headerEnd:]...)
}

func TestChatMedia(t *testing.T) {
	photo, err := base64.StdEncoding.DecodeString(tinyPNG)
	require.NoError(t, err)

	guru := registerGuru(t, "Kenanga")
	_, code := classOf(t, guru)
	parent, _ := registerWali(t, code, "Media Child")
	other, _ := registerWali(t, code, "Other Media Child")
	stranger := registerGuru(t, "Seruni")

	parentID := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", guru.Token, nil), "Media Child")["id"].(string)
	guruID := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", parent.Token, nil), "")["id"].(string)

	started := call(t, http.MethodPost, chatBase+"/conversations", parent.Token, map[string]any{"userId": guruID})
	require.Equal(t, http.StatusCreated, started.Status, "body %v", started.Body)

	conversationID := started.data("id").(string)
	base := chatBase + "/conversations/" + conversationID
	pendingPrefix := "pending/chat/" + conversationID + "/" + parentID + "/"

	send := func(token string, body map[string]any) result {
		res, _ := callKey(t, http.MethodPost, base+"/messages", token, uuid.NewString(), body)

		return res
	}

	var photoKey, photoMessage string

	t.Run("a photo is checked, stripped of its metadata and kept for the sender until it is sent", func(t *testing.T) {
		res := uploadTo(t, base+"/media/photo", parent.Token, nil, "p.png", photo)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		photoKey = res.data("mediaKey").(string)

		require.True(t, strings.HasPrefix(photoKey, pendingPrefix), photoKey)
		require.Equal(t, "photo", res.data("kind"))
		require.Equal(t, "image/png", res.data("contentType"))
		require.Contains(t, uploaded(privateKeyPrefix+"pending/chat/"), privateKeyPrefix+photoKey, "it is in the private bucket")
		require.Empty(t, uploaded(photoKey), "and not in the public one")

		// a photo is far larger than the 64 KB that any other body may have, and it must arrive
		large := paddedPNG(t, photo, 200*1024)
		bigRes := uploadTo(t, base+"/media/photo", parent.Token, nil, "p.png", large)
		require.Equal(t, http.StatusOK, bigRes.Status, "body %v", bigRes.Body)
		require.Less(t, bigRes.data("sizeBytes"), float64(len(large)), "the text chunk was cut out")

		uploadTo(t, base+"/media/photo", parent.Token, nil, "p.png", []byte("not an image")).requireError(t, http.StatusBadRequest, "INVALID_IMAGE")
		uploadTo(t, base+"/media/photo", parent.Token, nil, "", nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		uploadTo(t, base+"/media/photo", "", nil, "p.png", photo).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_INVALID")
		uploadTo(t, base+"/media/photo", other.Token, nil, "p.png", photo).requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
		uploadTo(t, chatBase+"/conversations/not-a-uuid/media/photo", parent.Token, nil, "p.png", photo).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a message carries the photo and the file moves to its permanent key", func(t *testing.T) {
		res := send(parent.Token, map[string]any{"mediaKey": photoKey})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		photoMessage = itoa(messageIDOf(t, res))

		require.Equal(t, "", res.data("body"), "a photo needs no text")
		require.Equal(t, "photo", res.data("media", "kind"))
		require.Equal(t, "image/png", res.data("media", "contentType"))
		require.EqualValues(t, len(photo), res.data("media", "sizeBytes"))

		address, _ := res.data("media", "url").(string)
		require.Contains(t, address, "/"+testPrivateBucket+"/chat/"+conversationID+"/")
		require.Contains(t, address, "X-Amz-Signature", "the address is signed")
		require.Contains(t, address, "X-Amz-Expires=3600")

		permanent := uploaded(privateKeyPrefix + "chat/" + conversationID + "/")
		require.Len(t, permanent, 1)
		require.Empty(t, uploaded(privateKeyPrefix+photoKey), "the staged file is gone")

		again := send(parent.Token, map[string]any{"mediaKey": photoKey})
		again.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("the media shows in the window and in the conversation list for the other person", func(t *testing.T) {
		window := call(t, http.MethodGet, base+"/messages", guru.Token, nil)
		require.Equal(t, http.StatusOK, window.Status, "body %v", window.Body)

		media := window.data("messages").([]any)[0].(map[string]any)["media"].(map[string]any)
		require.Equal(t, "photo", media["kind"])
		require.NotEmpty(t, media["url"])
		require.NotEmpty(t, media["expiresAt"])

		last := call(t, http.MethodGet, chatBase+"/conversations", guru.Token, nil).data("conversations").([]any)[0].(map[string]any)["lastMessage"].(map[string]any)
		require.Equal(t, "photo", last["media"].(map[string]any)["kind"])
	})

	t.Run("a key that was not issued to the sender for this conversation is refused", func(t *testing.T) {
		send(guru.Token, map[string]any{"mediaKey": photoKey}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		send(parent.Token, map[string]any{"mediaKey": pendingPrefix + uuid.NewString() + ".png"}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		send(parent.Token, map[string]any{"mediaKey": "avatars/" + parentID + "/a.png"}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		send(parent.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		send(stranger.Token, map[string]any{"mediaKey": photoKey}).requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")
	})

	t.Run("a video goes straight to the bucket through a signed address and is sent with text", func(t *testing.T) {
		call(t, http.MethodPost, base+"/media/video-upload", parent.Token, map[string]any{"contentType": "video/avi", "sizeBytes": 10}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, base+"/media/video-upload", parent.Token, map[string]any{"contentType": "video/mp4", "sizeBytes": 26 * 1024 * 1024}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, base+"/media/video-upload", other.Token, map[string]any{"contentType": "video/mp4", "sizeBytes": 10}).
			requireError(t, http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND")

		res := call(t, http.MethodPost, base+"/media/video-upload", parent.Token, map[string]any{"contentType": "video/mp4", "sizeBytes": 2048})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		key := res.data("mediaKey").(string)

		require.True(t, strings.HasPrefix(key, pendingPrefix) && strings.HasSuffix(key, ".mp4"), key)
		require.Equal(t, "PUT", res.data("method"))

		// the message cannot go before the file arrives
		send(parent.Token, map[string]any{"mediaKey": key}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, res.data("uploadUrl").(string), bytes.NewReader(make([]byte, 2048)))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "video/mp4")

		put, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, put.Body.Close())
		require.Equal(t, http.StatusOK, put.StatusCode)

		sentVideo := send(parent.Token, map[string]any{"body": "Rani singing", "mediaKey": key})
		require.Equal(t, http.StatusCreated, sentVideo.Status, "body %v", sentVideo.Body)
		require.Equal(t, "Rani singing", sentVideo.data("body"))
		require.Equal(t, "video", sentVideo.data("media", "kind"))
		require.EqualValues(t, 2048, sentVideo.data("media", "sizeBytes"))
	})

	t.Run("deleting a message removes its media and queues the file", func(t *testing.T) {
		var before []string

		require.NoError(t, testDB.Raw("SELECT media_key FROM chat_messages WHERE id = ?", photoMessage).Scan(&before).Error)
		require.Len(t, before, 1)

		res := call(t, http.MethodDelete, base+"/messages/"+photoMessage, parent.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, true, res.data("deleted"))
		require.Nil(t, res.data("media"))

		var queued int64

		require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM file_cleanups WHERE object_key = ?", before[0]).Scan(&queued).Error)
		require.EqualValues(t, 1, queued, "the file is queued in the transaction of the delete")

		// nothing sweeps the queue in this run, and the sweep test expects only its own keys in it, so do what the sweep would
		require.NoError(t, testDB.Exec("DELETE FROM file_cleanups WHERE object_key = ?", before[0]).Error)
		forgetUploaded(privateKeyPrefix + before[0])

		var kept []string

		require.NoError(t, testDB.Raw("SELECT media_key FROM chat_messages WHERE id = ? AND media_key IS NOT NULL", photoMessage).Scan(&kept).Error)
		require.Empty(t, kept)
	})
}
