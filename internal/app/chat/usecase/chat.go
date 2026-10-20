// Package usecase holds the rules of the chat between teachers and parents
package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/chat/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
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
	// StartConversation returns the conversation with a person, or the room of a class, and whether this call made it. A new direct
	// conversation needs a shared class and a room needs the account to belong to the class.
	StartConversation(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.StartChatRequest) (dto.ChatConversation, bool, error)
	// ListConversations returns the conversations that have a message, the newest message first
	ListConversations(ctx context.Context, userID uuid.UUID, page pagination.Params) (dto.ChatConversationList, error)
	// ListMessages returns a window of a conversation in the order the messages were sent, see ChatMessageList
	ListMessages(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, after *int64, before *int64, limit int) (dto.ChatMessageList, error)
	// UploadPhoto checks a photo, strips its metadata and stores it in the private bucket until a message carries it
	UploadPhoto(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, data []byte) (dto.ChatPhotoUpload, error)
	// CreateVideoUpload returns a signed address that takes one video straight into the private bucket
	CreateVideoUpload(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.ChatVideoUploadRequest) (dto.ChatVideoUpload, error)
	// SendMessage writes a message with text, a photo or video, or both, while the conversation is open
	SendMessage(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.SendChatMessageRequest) (dto.ChatMessage, error)
	// DeleteMessage empties a message of the signed in account and leaves a placeholder, deleting it again changes nothing
	DeleteMessage(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, messageID int64) (dto.ChatMessage, error)
	// MarkRead moves the read mark of the account forward
	MarkRead(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.MarkChatReadRequest) (dto.ChatReadState, error)
}

type ChatUseCase struct {
	repo repository.ChatDBItf
	// storage is the private bucket of the chat media, and it is disabled when the bucket is not configured
	storage s3.StorageItf
	cfg     *env.Env
	now     func() time.Time
}

func NewChatUseCase(repo repository.ChatDBItf, storage s3.StorageItf, cfg *env.Env) ChatUseCaseItf {
	return &ChatUseCase{repo: repo, storage: storage, cfg: cfg, now: time.Now}
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
	if (req.UserID == nil) == (req.ClassID == nil) {
		return dto.ChatConversation{}, false, apperror.Validation(map[string]string{"userId": "send either userId or classId"})
	}

	_, ok := peerRole(role)
	if !ok {
		return dto.ChatConversation{}, false, apperror.ErrForbidden
	}

	if req.ClassID != nil {
		return u.openRoom(ctx, userID, *req.ClassID)
	}

	return u.openDirect(ctx, userID, role, *req.UserID)
}

// openRoom returns the room of a class for one of its members. A class that nobody opened a room for gets one, unless it is
// archived, and the answer for a class the account does not belong to is the same as for a class that does not exist.
func (u *ChatUseCase) openRoom(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (dto.ChatConversation, bool, error) {
	membership, err := u.repo.MemberSince(ctx, userID, classID)
	if err != nil {
		return dto.ChatConversation{}, false, apperror.Internal(err)
	}

	if membership == nil {
		return dto.ChatConversation{}, false, apperror.ErrChatContactNotAllowed
	}

	room, err := u.repo.FindClassRoom(ctx, classID)
	if err != nil {
		return dto.ChatConversation{}, false, apperror.Internal(err)
	}

	created := false

	if room == nil {
		// the room of an archived class that was never opened would only ever be empty and closed
		if membership.Archived {
			return dto.ChatConversation{}, false, apperror.ErrChatContactNotAllowed
		}

		room, created, err = u.repo.GetOrCreateClassRoom(ctx, classID, u.timestamp())
		if err != nil {
			return dto.ChatConversation{}, false, apperror.Internal(err)
		}

		if room == nil {
			return dto.ChatConversation{}, false, apperror.Internal(errors.New("the class room is gone after it was opened"))
		}
	}

	view, err := u.viewOf(ctx, userID, room.ID)

	return view, created, err
}

func (u *ChatUseCase) openDirect(
	ctx context.Context, userID uuid.UUID, role constants.Role, peerID uuid.UUID,
) (dto.ChatConversation, bool, error) {
	if peerID == userID {
		return dto.ChatConversation{}, false, apperror.Validation(map[string]string{"userId": "cannot be your own account"})
	}

	wanted, _ := peerRole(role)

	// a person who does not exist, was deleted or has the same role gets the same answer, so the request does not tell
	// whether an id belongs to an account
	peer, err := u.repo.FindAccount(ctx, peerID)
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

	return u.conversationResponse(ctx, row), nil
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
		res.Conversations = append(res.Conversations, u.conversationResponse(ctx, &rows[i]))
	}

	return res, nil
}

// member checks that the account belongs to the conversation, as one of the two people of a direct one or as a member of the
// class of a room, and returns the time from which it sees messages. That is the zero time for a direct conversation. Anyone
// else gets not found, so the answer does not tell whether a conversation exists.
func (u *ChatUseCase) member(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID) (time.Time, error) {
	conversation, err := u.repo.FindConversation(ctx, conversationID)
	if err != nil {
		return time.Time{}, apperror.Internal(err)
	}

	if conversation == nil {
		return time.Time{}, apperror.ErrChatConversationNotFound
	}

	if conversation.Kind == constants.ChatKindClass {
		if conversation.ClassID == nil {
			return time.Time{}, apperror.ErrChatConversationNotFound
		}

		membership, memberErr := u.repo.MemberSince(ctx, userID, *conversation.ClassID)
		if memberErr != nil {
			return time.Time{}, apperror.Internal(memberErr)
		}

		if membership == nil {
			return time.Time{}, apperror.ErrChatConversationNotFound
		}

		return membership.Since, nil
	}

	if conversation.UserLowID == nil || conversation.UserHighID == nil || (*conversation.UserLowID != userID && *conversation.UserHighID != userID) {
		return time.Time{}, apperror.ErrChatConversationNotFound
	}

	return time.Time{}, nil
}

func (u *ChatUseCase) ListMessages(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, after *int64, before *int64, limit int,
) (dto.ChatMessageList, error) {
	since, err := u.member(ctx, userID, conversationID)
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

	messages, more, err := u.repo.ListMessages(ctx, conversationID, since, after, before, limit)
	if err != nil {
		return dto.ChatMessageList{}, apperror.Internal(err)
	}

	res := dto.ChatMessageList{Messages: make([]dto.ChatMessage, 0, len(messages)), HasMore: more}
	for i := range messages {
		res.Messages = append(res.Messages, u.messageResponse(ctx, &messages[i]))
	}

	return res, nil
}

// photoTypes and videoTypes map each accepted type to the extension of its object key
var (
	photoTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}
	videoTypes = map[string]string{"video/mp4": ".mp4", "video/webm": ".webm"}
)

const (
	bytesPerMB = 1024 * 1024

	// pendingPrefix holds the files that were uploaded and not sent in a message yet, and the bucket expires it after a day
	pendingPrefix = "pending/"
	// mediaDirectory holds the files of sent messages, and it is also how the cleanup job knows a key belongs to the private bucket
	mediaDirectory = filecleanup.PrivatePrefix
)

// pendingKeyPrefix is where the uploads of one account for one conversation wait. The key of a message has to start with it, so
// a key of another conversation or of another account is refused before the bucket is asked about it.
func pendingKeyPrefix(conversationID uuid.UUID, userID uuid.UUID) string {
	return pendingPrefix + mediaDirectory + conversationID.String() + "/" + userID.String() + "/"
}

// openForUpload checks that the account is in the conversation, that the conversation still takes messages and that the
// private bucket is there, so a file is never stored for a conversation that cannot use it
func (u *ChatUseCase) openForUpload(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID) error {
	_, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return err
	}

	if !u.storage.Enabled() {
		return apperror.ErrStorageDisabled
	}

	view, err := u.viewOf(ctx, userID, conversationID)
	if err != nil {
		return err
	}

	if !view.CanSend {
		return apperror.ErrChatSendingClosed
	}

	return nil
}

func (u *ChatUseCase) UploadPhoto(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, data []byte,
) (dto.ChatPhotoUpload, error) {
	err := u.openForUpload(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatPhotoUpload{}, err
	}

	image, err := imageutil.Inspect(data, u.cfg.ChatPhotoMaxMB*bytesPerMB)
	if err != nil {
		return dto.ChatPhotoUpload{}, err
	}

	key := pendingKeyPrefix(conversationID, userID) + uuid.NewString() + image.Extension

	_, err = u.storage.Upload(ctx, key, image.ContentType, image.Data)
	if err != nil {
		return dto.ChatPhotoUpload{}, err
	}

	return dto.ChatPhotoUpload{MediaKey: key, Kind: "photo", ContentType: image.ContentType, SizeBytes: int64(len(image.Data))}, nil
}

func (u *ChatUseCase) CreateVideoUpload(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.ChatVideoUploadRequest,
) (dto.ChatVideoUpload, error) {
	err := u.openForUpload(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatVideoUpload{}, err
	}

	maxBytes := int64(u.cfg.ChatVideoMaxMB) * bytesPerMB
	if req.SizeBytes > maxBytes {
		return dto.ChatVideoUpload{}, apperror.Validation(map[string]string{
			"sizeBytes": fmt.Sprintf("must be at most %d bytes", maxBytes),
		})
	}

	key := pendingKeyPrefix(conversationID, userID) + uuid.NewString() + videoTypes[req.ContentType]
	ttl := time.Duration(u.cfg.VideoUploadURLSeconds) * time.Second

	uploadURL, err := u.storage.PresignUpload(ctx, key, req.ContentType, req.SizeBytes, ttl)
	if err != nil {
		return dto.ChatVideoUpload{}, err
	}

	return dto.ChatVideoUpload{
		UploadURL: uploadURL,
		Method:    http.MethodPut,
		Headers:   map[string]string{"Content-Type": req.ContentType},
		MediaKey:  key,
		ExpiresAt: u.now().Add(ttl).UTC(),
	}, nil
}

// stagedMedia is a finished upload that a message is about to carry
type stagedMedia struct {
	pendingKey string
	finalKey   string
	kind       string
	object     *s3.Object
}

// stage makes sure the key is a finished upload of this account for this conversation, that its type and size are accepted, and
// copies it to the permanent key. The caller removes the copy when the message cannot be stored.
func (u *ChatUseCase) stage(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, mediaKey string) (*stagedMedia, error) {
	// the name after the prefix is the one that an upload returned, so it has no path in it
	name, ours := strings.CutPrefix(mediaKey, pendingKeyPrefix(conversationID, userID))
	if !ours || name == "" || strings.Contains(name, "/") {
		return nil, apperror.Validation(map[string]string{"mediaKey": "is not an upload of yours for this conversation"})
	}

	if !u.storage.Enabled() {
		return nil, apperror.ErrStorageDisabled
	}

	object, err := u.storage.Stat(ctx, mediaKey)
	if err != nil {
		return nil, err
	}

	if object == nil {
		return nil, apperror.Validation(map[string]string{"mediaKey": "has no uploaded file yet"})
	}

	kind, limit := "", int64(0)

	switch {
	case photoTypes[object.ContentType] != "":
		kind, limit = "photo", int64(u.cfg.ChatPhotoMaxMB)*bytesPerMB
	case videoTypes[object.ContentType] != "":
		kind, limit = "video", int64(u.cfg.ChatVideoMaxMB)*bytesPerMB
	}

	if kind == "" || object.Size < 1 || object.Size > limit {
		return nil, apperror.Validation(map[string]string{"mediaKey": "is not an accepted photo or video"})
	}

	finalKey := mediaDirectory + conversationID.String() + "/" + uuid.NewString() + path.Ext(mediaKey)

	err = u.storage.Copy(ctx, mediaKey, finalKey, object.ContentType)
	if err != nil {
		return nil, err
	}

	return &stagedMedia{pendingKey: mediaKey, finalKey: finalKey, kind: kind, object: object}, nil
}

func (u *ChatUseCase) SendMessage(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.SendChatMessageRequest,
) (dto.ChatMessage, error) {
	_, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatMessage{}, err
	}

	body, mediaKey := strings.TrimSpace(req.Body), strings.TrimSpace(req.MediaKey)
	if body == "" && mediaKey == "" {
		return dto.ChatMessage{}, apperror.Validation(map[string]string{"body": "is required when no mediaKey is sent"})
	}

	// reading is always open to the people of a conversation, writing needs it to be open and the account to be there
	view, err := u.viewOf(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatMessage{}, err
	}

	if !view.CanSend {
		return dto.ChatMessage{}, apperror.ErrChatSendingClosed
	}

	message := entity.ChatMessage{ConversationID: conversationID, SenderID: userID, Body: body, CreatedAt: u.timestamp()}

	var staged *stagedMedia

	if mediaKey != "" {
		staged, err = u.stage(ctx, userID, conversationID, mediaKey)
		if err != nil {
			return dto.ChatMessage{}, err
		}

		message.MediaKey, message.MediaKind = &staged.finalKey, &staged.kind
		message.MediaType, message.MediaSize = &staged.object.ContentType, &staged.object.Size
	}

	err = u.repo.CreateMessage(ctx, &message)
	if err != nil {
		if staged != nil {
			s3.Discard(ctx, u.storage, staged.finalKey)
		}

		if errors.Is(err, repository.ErrConversationGone) {
			return dto.ChatMessage{}, apperror.ErrChatConversationNotFound
		}

		return dto.ChatMessage{}, apperror.Internal(err)
	}

	if staged != nil {
		// the staged file is no longer needed, and the lifecycle rule of the bucket removes it within a day if this fails
		s3.Discard(ctx, u.storage, staged.pendingKey)
	}

	// the stored message is read again so that it carries the person who wrote it, in the shape every other answer has
	stored, err := u.repo.FindMessage(ctx, conversationID, message.ID, time.Time{})
	if err != nil {
		return dto.ChatMessage{}, apperror.Internal(err)
	}

	if stored == nil {
		return dto.ChatMessage{}, apperror.ErrChatMessageNotFound
	}

	return u.messageResponse(ctx, stored), nil
}

func (u *ChatUseCase) DeleteMessage(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, messageID int64,
) (dto.ChatMessage, error) {
	since, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatMessage{}, err
	}

	// a message from before the account joined a room is not found, the same answer as for an id that does not exist
	message, err := u.repo.FindMessage(ctx, conversationID, messageID, since)
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

	message, err = u.repo.FindMessage(ctx, conversationID, messageID, since)
	if err != nil {
		return dto.ChatMessage{}, apperror.Internal(err)
	}

	if message == nil {
		return dto.ChatMessage{}, apperror.ErrChatMessageNotFound
	}

	return u.messageResponse(ctx, message), nil
}

func (u *ChatUseCase) MarkRead(
	ctx context.Context, userID uuid.UUID, conversationID uuid.UUID, req dto.MarkChatReadRequest,
) (dto.ChatReadState, error) {
	since, err := u.member(ctx, userID, conversationID)
	if err != nil {
		return dto.ChatReadState{}, err
	}

	mark, unread, err := u.repo.MarkRead(ctx, conversationID, userID, since, req.MessageID, u.timestamp())
	if err != nil {
		return dto.ChatReadState{}, apperror.Internal(err)
	}

	return dto.ChatReadState{LastReadMessageID: mark, UnreadCount: unread}, nil
}

// personOf turns the columns of an account into a person, and a deleted account shows as Deleted account without a photo
func personOf(id uuid.UUID, name string, role constants.Role, avatar *string, deleted bool) dto.ChatPerson {
	person := dto.ChatPerson{ID: id, FullName: name, Role: role, AvatarURL: avatar, Deleted: deleted}

	if deleted {
		person.FullName = DeletedAccountName
		person.AvatarURL = nil
	}

	return person
}

// mediaOf turns the media columns of a message into the photo or video with a signed read address, and nil for a message without
// one. A file whose address cannot be signed is still reported, with an empty address, so one failure does not hide the others.
func (u *ChatUseCase) mediaOf(ctx context.Context, key *string, kind *string, contentType *string, size *int64) *dto.ChatMedia {
	if key == nil || kind == nil || contentType == nil || size == nil {
		return nil
	}

	ttl := time.Duration(u.cfg.ChatMediaURLSeconds) * time.Second
	media := &dto.ChatMedia{Kind: *kind, ContentType: *contentType, SizeBytes: *size, ExpiresAt: u.now().Add(ttl).UTC()}

	address, err := u.storage.PresignRead(ctx, *key, ttl)
	if err != nil {
		log.Printf("signing the read address of chat media failed %v", err)

		media.ExpiresAt = u.now().UTC()

		return media
	}

	media.URL = address

	return media
}

func (u *ChatUseCase) messageResponse(ctx context.Context, message *repository.MessageRow) dto.ChatMessage {
	return dto.ChatMessage{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		SenderID:       message.SenderID,
		Sender:         personOf(message.SenderID, message.SenderName, message.SenderRole, message.SenderAvatarURL, message.SenderDeleted),
		Body:           message.Body,
		Media:          u.mediaOf(ctx, message.MediaKey, message.MediaKind, message.MediaType, message.MediaSize),
		Deleted:        message.DeletedAt != nil,
		CreatedAt:      message.CreatedAt.UTC(),
	}
}

func (u *ChatUseCase) conversationResponse(ctx context.Context, row *repository.ConversationRow) dto.ChatConversation {
	res := dto.ChatConversation{
		ID:             row.ID,
		Kind:           row.Kind,
		UnreadCount:    row.UnreadCount,
		PeerLastReadID: row.PeerLastReadMessageID,
		CanSend:        row.CanSend,
		CreatedAt:      row.CreatedAt.UTC(),
	}

	if row.Kind == constants.ChatKindClass {
		if row.ClassID != nil && row.ClassName != nil {
			res.Class = &dto.ChatContactClass{ID: *row.ClassID, Name: *row.ClassName}
		}
	} else if row.PeerID != nil && row.PeerName != nil && row.PeerRole != nil && row.PeerDeleted != nil {
		peer := personOf(*row.PeerID, *row.PeerName, *row.PeerRole, row.PeerAvatarURL, *row.PeerDeleted)
		res.Peer = &peer
	}

	if row.LastID != nil && row.LastSenderID != nil && row.LastBody != nil && row.LastDeleted != nil && row.LastCreatedAt != nil &&
		row.LastSenderName != nil && row.LastSenderRole != nil && row.LastSenderDeleted != nil {
		res.LastMessage = &dto.ChatMessage{
			ID:             *row.LastID,
			ConversationID: row.ID,
			SenderID:       *row.LastSenderID,
			Sender: personOf(
				*row.LastSenderID, *row.LastSenderName, *row.LastSenderRole, row.LastSenderAvatarURL, *row.LastSenderDeleted,
			),
			Body:      *row.LastBody,
			Media:     u.mediaOf(ctx, row.LastMediaKey, row.LastMediaKind, row.LastMediaType, row.LastMediaSize),
			Deleted:   *row.LastDeleted,
			CreatedAt: row.LastCreatedAt.UTC(),
		}
	}

	return res
}
