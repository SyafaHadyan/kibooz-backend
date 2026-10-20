package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// ChatPerson is the other member of a conversation or a contact. Deleted is true for an account that was deleted, which
// shows as a person with the name Deleted account.
type ChatPerson struct {
	ID        uuid.UUID      `json:"id"`
	FullName  string         `json:"fullName"`
	Role      constants.Role `json:"role"`
	AvatarURL *string        `json:"avatarUrl"`
	Deleted   bool           `json:"deleted"`
}

// ChatContactClass is a class that a contact shares with the signed in account
type ChatContactClass struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ChatContact is a person the signed in account can start a conversation with, because they share an active class.
// Children holds the names of the children of the parent in the shared classes, for a teacher this is the child of the contact
// and for a parent it is their own child.
type ChatContact struct {
	ChatPerson
	Classes  []ChatContactClass `json:"classes"`
	Children []string           `json:"children"`
	// ConversationID is empty until the first conversation with this person exists
	ConversationID *uuid.UUID `json:"conversationId"`
}

type ChatContactList struct {
	Contacts []ChatContact `json:"contacts"`
	PageInfo
}

type StartChatRequest struct {
	UserID uuid.UUID `json:"userId" validate:"required"`
}

// ChatMessage is a message of a conversation. A deleted message has an empty body and Deleted set.
type ChatMessage struct {
	ID             int64     `json:"id"`
	ConversationID uuid.UUID `json:"conversationId"`
	SenderID       uuid.UUID `json:"senderId"`
	Body           string    `json:"body"`
	Deleted        bool      `json:"deleted"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ChatConversation is a conversation as the signed in account sees it. CanSend is false when the two no longer share a class
// or the other account was deleted, and the history stays readable.
type ChatConversation struct {
	ID             uuid.UUID    `json:"id"`
	Peer           ChatPerson   `json:"peer"`
	LastMessage    *ChatMessage `json:"lastMessage"`
	UnreadCount    int          `json:"unreadCount"`
	PeerLastReadID int64        `json:"peerLastReadMessageId"`
	CanSend        bool         `json:"canSend"`
	CreatedAt      time.Time    `json:"createdAt"`
}

type ChatConversationList struct {
	Conversations []ChatConversation `json:"conversations"`
	PageInfo
}

type SendChatMessageRequest struct {
	Body string `json:"body" validate:"required,max=4000"`
}

// ChatMessageList is a window of a conversation in the order the messages were sent. HasMore tells whether there are more
// messages in the direction that was asked for, older ones for before and newer ones for after.
type ChatMessageList struct {
	Messages []ChatMessage `json:"messages"`
	HasMore  bool          `json:"hasMore"`
}

type MarkChatReadRequest struct {
	MessageID int64 `json:"messageId" validate:"min=0"`
}

// ChatReadState is what a read mark leaves, the message the account has read up to and how many messages from the other side are unread
type ChatReadState struct {
	LastReadMessageID int64 `json:"lastReadMessageId"`
	UnreadCount       int   `json:"unreadCount"`
}
