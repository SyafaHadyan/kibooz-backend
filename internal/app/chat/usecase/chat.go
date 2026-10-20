// Package usecase holds the rules of the chat between teachers and parents
package usecase

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/chat/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

const (
	// DeletedAccountName is shown for a person whose account was deleted, the same name the forum uses
	DeletedAccountName = "Deleted account"

	// DefaultMessageLimit and MaxMessageLimit bound a window of messages
	DefaultMessageLimit = 50
	MaxMessageLimit     = 100
)

type ChatUseCaseItf interface {
	// ListContacts returns the people the account can start a conversation with, the parents of the classes of a teacher or the
	// teachers of the classes of a parent
	ListContacts(ctx context.Context, userID uuid.UUID, role constants.Role, page pagination.Params) (dto.ChatContactList, error)
	// StartConversation returns the conversation with a person and whether this call made it. A new one needs a shared class.
	StartConversation(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.StartChatRequest) (dto.ChatConversation, bool, error)
	// ListConversations returns the conversations that have a message, the newest message first
	ListConversations(ctx context.Context, userID uuid.UUID, page pagination.Params) (dto.ChatConversationList, error)
	// ListMessages returns a window of a conversation in the order the messages were sent, see ChatMessageList
	ListMessages(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, after *int64, before *int64, limit int) (dto.ChatMessageList, error)
	// SendMessage writes a message while the two still share a class
	SendMessage(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.SendChatMessageRequest) (dto.ChatMessage, error)
	// DeleteMessage empties a message of the signed in account and leaves a placeholder, deleting it again changes nothing
	DeleteMessage(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, messageID int64) (dto.ChatMessage, error)
	// MarkRead moves the read mark of the account forward
	MarkRead(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.MarkChatReadRequest) (dto.ChatReadState, error)
}

type ChatUseCase struct {
	repo repository.ChatDBItf
	now  func() time.Time
}

func NewChatUseCase(repo repository.ChatDBItf) ChatUseCaseItf {
	return &ChatUseCase{repo: repo, now: time.Now}
}

// timestamp keeps the precision PostgreSQL stores, so the order of two quick messages is stable
func (u *ChatUseCase) timestamp() time.Time {
	return u.now().UTC().Truncate(time.Microsecond)
}

// peerRole is the role of the people a role can talk to, and false for a role that cannot use the chat
func peerRole(role constants.Role) (constants.Role, bool) {
	switch role {
	case constants.RoleGuru:
		return constants.RoleWali, true
	case constants.RoleWali:
		return constants.RoleGuru, true
	default:
		return "", false
	}
}

func (u *ChatUseCase) ListContacts(
	ctx context.Context, userID uuid.UUID, role constants.Role, page pagination.Params,
) (dto.ChatContactList, error) {
	peers, ok := peerRole(role)
	if !ok {
		return dto.ChatContactList{}, apperror.ErrForbidden
	}

	rows, total, err := u.repo.ListContacts(ctx, userID, peers, page.Limit, page.Offset())
	if err != nil {
		return dto.ChatContactList{}, apperror.Internal(err)
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].UserID)
	}

	links, err := u.repo.ContactLinks(ctx, userID, ids)
	if err != nil {
		return dto.ChatContactList{}, apperror.Internal(err)
	}

	conversations, err := u.repo.ConversationLinks(ctx, userID, ids)
	if err != nil {
		return dto.ChatContactList{}, apperror.Internal(err)
	}

	existing := make(map[uuid.UUID]uuid.UUID, len(conversations))
	for _, link := range conversations {
		existing[link.PeerID] = link.ID
	}

	res := dto.ChatContactList{
		Contacts: make([]dto.ChatContact, 0, len(rows)),
		PageInfo: dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range rows {
		contact := dto.ChatContact{
			ChatPerson: dto.ChatPerson{ID: rows[i].UserID, FullName: rows[i].FullName, Role: rows[i].Role, AvatarURL: rows[i].AvatarURL},
			Classes:    []dto.ChatContactClass{},
			Children:   []string{},
		}

		if id, found := existing[rows[i].UserID]; found {
			contact.ConversationID = &id
		}

		seenClass, seenChild := map[uuid.UUID]bool{}, map[string]bool{}

		for _, link := range links {
			if link.PeerID != rows[i].UserID {
				continue
			}

			if !seenClass[link.ClassID] {
				seenClass[link.ClassID] = true
				contact.Classes = append(contact.Classes, dto.ChatContactClass{ID: link.ClassID, Name: link.ClassName})
			}

			if !seenChild[link.ChildName] {
				seenChild[link.ChildName] = true
				contact.Children = append(contact.Children, link.ChildName)
			}
		}

		res.Contacts = append(res.Contacts, contact)
	}

	return res, nil
}

func (u *ChatUseCase) StartConversation(
	ctx context.Context, userID uuid.UUID, role constants.Role, req dto.StartChatRequest,
) (dto.ChatConversation, bool, error) {
	if req.UserID == userID {
		return dto.ChatConversation{}, false, apperror.Validation(map[string]string{"userId": "cannot be your own account"})
	}

	wanted, ok := peerRole(role)
	if !ok {
		return dto.ChatConversation{}, false, apperror.ErrForbidden
	}

	// a person who does not exist, was deleted or has the same role gets the same answer, so the request does not tell
	// whether an id belongs to an account
	peer, err := u.repo.FindAccount(ctx, req.UserID)
	if err != nil {
		return dto.ChatConversation{}, false, apperror.Internal(err)
	}

	if peer == nil || peer.Role != wanted {
		return dto.ChatConversation{}, false, apperror.ErrChatContactNotAllowed
	}

	shared, err := u.repo.ShareClass(ctx, userID, peer.ID)
	if err != nil {
		return dto.ChatConversation{}, false, apperror.Internal(err)
	}

	if !shared {
		// the old conversation of two people who no longer share a class stays readable, so it can still be opened
		links, linkErr := u.repo.ConversationLinks(ctx, userID, []uuid.UUID{peer.ID})
		if linkErr != nil {
			return dto.ChatConversation{}, false, apperror.Internal(linkErr)
		}

		if len(links) == 0 {
			return dto.ChatConversation{}, false, apperror.ErrChatContactNotAllowed
		}

		view, viewErr := u.viewOf(ctx, userID, links[0].ID)

		return view, false, viewErr
	}

	low, high := orderedPair(userID, peer.ID)

	conversation, created, err := u.repo.GetOrCreateConversation(ctx, low, high, u.timestamp())
	if err != nil {
		return dto.ChatConversation{}, false, apperror.Internal(err)
	}

	view, err := u.viewOf(ctx, userID, conversation.ID)

	return view, created, err
}

// orderedPair puts the smaller id first, in the order PostgreSQL compares two uuids
func orderedPair(a uuid.UUID, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if bytes.Compare(a[:], b[:]) < 0 {
		return a, b
	}

	return b, a
}

// viewOf reads a conversation the way the account sees it
func (u *ChatUseCase) viewOf(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID) (dto.ChatConversation, error) {
	row, err := u.repo.FindConversationFor(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatConversation{}, apperror.Internal(err)
	}

	if row == nil {
		return dto.ChatConversation{}, apperror.ErrChatConversationNotFound
	}

	return conversationResponse(row), nil
}

func (u *ChatUseCase) ListConversations(
	ctx context.Context, userID uuid.UUID, page pagination.Params,
) (dto.ChatConversationList, error) {
	rows, total, err := u.repo.ListConversations(ctx, userID, page.Limit, page.Offset())
	if err != nil {
		return dto.ChatConversationList{}, apperror.Internal(err)
	}

	res := dto.ChatConversationList{
		Conversations: make([]dto.ChatConversation, 0, len(rows)),
		PageInfo:      dto.PageInfo{Page: page.Page, Limit: page.Limit, Total: total},
	}

	for i := range rows {
		res.Conversations = append(res.Conversations, conversationResponse(&rows[i]))
	}

	return res, nil
}

// member returns the conversation when the account is one of its two people, and not found for anyone else, so the answer does
// not tell whether a conversation exists
func (u *ChatUseCase) member(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID) (*entity.ChatConversation, error) {
	conversation, err := u.repo.FindConversation(ctx, conversationID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if conversation == nil || (conversation.UserLowID != userID && conversation.UserHighID != userID) {
		return nil, apperror.ErrChatConversationNotFound
	}

	return conversation, nil
}

func (u *ChatUseCase) ListMessages(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, after *int64, before *int64, limit int,
) (dto.ChatMessageList, error) {
	_, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatMessageList{}, err
	}

	details := map[string]string{}

	if after != nil && before != nil {
		details["before"] = "cannot be sent together with after"
	}

	if after != nil && *after < 0 {
		details["after"] = "must be 0 or a message id"
	}

	if before != nil && *before < 1 {
		details["before"] = "must be a message id"
	}

	if limit < 1 || limit > MaxMessageLimit {
		details["limit"] = "must be a whole number from 1 to 100"
	}

	if len(details) > 0 {
		return dto.ChatMessageList{}, apperror.Validation(details)
	}

	messages, more, err := u.repo.ListMessages(ctx, conversationID, after, before, limit)
	if err != nil {
		return dto.ChatMessageList{}, apperror.Internal(err)
	}

	res := dto.ChatMessageList{Messages: make([]dto.ChatMessage, 0, len(messages)), HasMore: more}
	for i := range messages {
		res.Messages = append(res.Messages, messageResponse(&messages[i]))
	}

	return res, nil
}

func (u *ChatUseCase) SendMessage(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.SendChatMessageRequest,
) (dto.ChatMessage, error) {
	_, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatMessage{}, err
	}

	body := strings.TrimSpace(req.Body)
	if body == "" {
		return dto.ChatMessage{}, apperror.Validation(map[string]string{"body": "is required"})
	}

	// reading is always open to the two people, writing needs a class they still share and an account that is still there
	view, err := u.viewOf(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatMessage{}, err
	}

	if !view.CanSend {
		return dto.ChatMessage{}, apperror.ErrChatSendingClosed
	}

	message := entity.ChatMessage{ConversationID: conversationID, SenderID: userID, Body: body, CreatedAt: u.timestamp()}

	err = u.repo.CreateMessage(ctx, &message)
	if errors.Is(err, repository.ErrConversationGone) {
		return dto.ChatMessage{}, apperror.ErrChatConversationNotFound
	}

	if err != nil {
		return dto.ChatMessage{}, apperror.Internal(err)
	}

	return messageResponse(&message), nil
}

func (u *ChatUseCase) DeleteMessage(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, messageID int64,
) (dto.ChatMessage, error) {
	_, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatMessage{}, err
	}

	message, err := u.repo.FindMessage(ctx, conversationID, messageID)
	if err != nil {
		return dto.ChatMessage{}, apperror.Internal(err)
	}

	if message == nil {
		return dto.ChatMessage{}, apperror.ErrChatMessageNotFound
	}

	// only the sender may remove a message, not even the other person in the conversation
	if message.SenderID != userID {
		return dto.ChatMessage{}, apperror.ErrForbidden
	}

	err = u.repo.DeleteMessage(ctx, conversationID, messageID, userID, u.timestamp())
	if err != nil {
		return dto.ChatMessage{}, apperror.Internal(err)
	}

	message, err = u.repo.FindMessage(ctx, conversationID, messageID)
	if err != nil {
		return dto.ChatMessage{}, apperror.Internal(err)
	}

	if message == nil {
		return dto.ChatMessage{}, apperror.ErrChatMessageNotFound
	}

	return messageResponse(message), nil
}

func (u *ChatUseCase) MarkRead(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.MarkChatReadRequest,
) (dto.ChatReadState, error) {
	_, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatReadState{}, err
	}

	mark, unread, err := u.repo.MarkRead(ctx, conversationID, userID, req.MessageID, u.timestamp())
	if err != nil {
		return dto.ChatReadState{}, apperror.Internal(err)
	}

	return dto.ChatReadState{LastReadMessageID: mark, UnreadCount: unread}, nil
}

func messageResponse(message *entity.ChatMessage) dto.ChatMessage {
	return dto.ChatMessage{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		SenderID:       message.SenderID,
		Body:           message.Body,
		Deleted:        message.DeletedAt != nil,
		CreatedAt:      message.CreatedAt.UTC(),
	}
}

func conversationResponse(row *repository.ConversationRow) dto.ChatConversation {
	peer := dto.ChatPerson{ID: row.PeerID, FullName: row.PeerName, Role: row.PeerRole, AvatarURL: row.PeerAvatarURL, Deleted: row.PeerDeleted}

	if row.PeerDeleted {
		peer.FullName = DeletedAccountName
		peer.AvatarURL = nil
	}

	res := dto.ChatConversation{
		ID:             row.ID,
		Peer:           peer,
		UnreadCount:    row.UnreadCount,
		PeerLastReadID: row.PeerLastReadMessageID,
		CanSend:        row.CanSend,
		CreatedAt:      row.CreatedAt.UTC(),
	}

	if row.LastID != nil && row.LastSenderID != nil && row.LastBody != nil && row.LastDeleted != nil && row.LastCreatedAt != nil {
		res.LastMessage = &dto.ChatMessage{
			ID:             *row.LastID,
			ConversationID: row.ID,
			SenderID:       *row.LastSenderID,
			Body:           *row.LastBody,
			Deleted:        *row.LastDeleted,
			CreatedAt:      row.LastCreatedAt.UTC(),
		}
	}

	return res
}
