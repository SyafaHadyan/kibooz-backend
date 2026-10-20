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

// StartChatRequest names the person to talk to, or the class whose room to open. Exactly one of the two is set.
type StartChatRequest struct {
	UserID  *uuid.UUID `json:"userId"`
	ClassID *uuid.UUID `json:"classId"`
}

// ChatMedia is the photo or video of a message. URL is a signed address that works until ExpiresAt, so the app caches the file by
// the id of the message and asks for the list again when the address is over.
type ChatMedia struct {
	Kind        string    `json:"kind"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// ChatMessage is a message of a conversation. A deleted message has an empty body and Deleted set. Sender is the person who
// wrote it, which is how a class room shows who spoke, and a deleted account shows as Deleted account. Media is nil for a
// message without a photo or a video, and for a deleted one.
type ChatMessage struct {
	ID             int64      `json:"id"`
	ConversationID uuid.UUID  `json:"conversationId"`
	SenderID       uuid.UUID  `json:"senderId"`
	Sender         ChatPerson `json:"sender"`
	Body           string     `json:"body"`
	Media          *ChatMedia `json:"media"`
	Deleted        bool       `json:"deleted"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// ChatConversation is a conversation as the signed in account sees it. Kind is direct, with Peer set, or class, with Class set.
// CanSend is false when a direct pair no longer shares a class, the other account was deleted or the class is archived, and the
// history stays readable. PeerLastReadID is 0 for a class room, which has no single other side.
type ChatConversation struct {
	ID             uuid.UUID         `json:"id"`
	Kind           string            `json:"kind"`
	Peer           *ChatPerson       `json:"peer"`
	Class          *ChatContactClass `json:"class"`
	LastMessage    *ChatMessage      `json:"lastMessage"`
	UnreadCount    int               `json:"unreadCount"`
	PeerLastReadID int64             `json:"peerLastReadMessageId"`
	CanSend        bool              `json:"canSend"`
	CreatedAt      time.Time         `json:"createdAt"`
}

type ChatConversationList struct {
	Conversations []ChatConversation `json:"conversations"`
	PageInfo
}

// SendChatMessageRequest has the text, the media or both. MediaKey is the key that an upload returned for this conversation.
type SendChatMessageRequest struct {
	Body     string `json:"body" validate:"max=4000"`
	MediaKey string `json:"mediaKey" validate:"max=300"`
}

// ChatPhotoUpload is a photo that was stored and waits for the message that carries it
type ChatPhotoUpload struct {
	MediaKey    string `json:"mediaKey"`
	Kind        string `json:"kind"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
}

type ChatVideoUploadRequest struct {
	ContentType string `json:"contentType" validate:"required,oneof=video/mp4 video/webm"`
	SizeBytes   int64  `json:"sizeBytes" validate:"required,gte=1"`
}

// ChatVideoUpload tells the app where to send the video, and the key to send with the message afterwards
type ChatVideoUpload struct {
	UploadURL string            `json:"uploadUrl"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	MediaKey  string            `json:"mediaKey"`
	ExpiresAt time.Time         `json:"expiresAt"`
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
