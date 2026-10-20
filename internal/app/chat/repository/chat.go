// Package repository handles the database operations of the chat
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// ErrConversationGone is returned when a message is stored in a conversation that no longer exists
var ErrConversationGone = errors.New("the conversation does not exist any more")

// AccountRow is the account that a conversation is started with
type AccountRow struct {
	ID        uuid.UUID
	Role      constants.Role
	FullName  string
	AvatarURL *string
}

// ContactRow is a person that shares an active class with the account
type ContactRow struct {
	UserID    uuid.UUID
	FullName  string
	Role      constants.Role
	AvatarURL *string
}

// ContactLinkRow ties a contact to one class and to the child that makes them share it
type ContactLinkRow struct {
	PeerID    uuid.UUID
	ClassID   uuid.UUID
	ClassName string
	ChildName string
}

// ConversationLinkRow is a conversation that exists between the account and a person
type ConversationLinkRow struct {
	ID     uuid.UUID
	PeerID uuid.UUID
}

// ConversationRow is a conversation with the other person, the last message and the numbers that depend on the account.
// The columns of the last message are nil for a conversation that has no message yet.
type ConversationRow struct {
	ID                    uuid.UUID
	CreatedAt             time.Time
	PeerID                uuid.UUID
	PeerName              string
	PeerRole              constants.Role
	PeerAvatarURL         *string
	PeerDeleted           bool
	LastID                *int64
	LastSenderID          *uuid.UUID
	LastBody              *string
	LastDeleted           *bool
	LastCreatedAt         *time.Time
	UnreadCount           int
	PeerLastReadMessageID int64
	CanSend               bool
}

// sharedClass is a condition that holds when a teacher and a parent have an active child in the same class that is not archived,
// in either order. It needs the id of the first account as the two ? marks and names the column of the second one in peer.
// With a ? as the peer the arguments are the first account, the peer, the peer and the first account.
func sharedClass(peer string) string {
	return `EXISTS (SELECT 1 FROM class_teachers AS ct
		JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL
		JOIN classes AS c ON c.id = ct.class_id AND c.archived_at IS NULL
		JOIN students AS s ON s.class_id = c.id AND s.deleted_at IS NULL
		JOIN walis AS w ON w.id = s.wali_id AND w.deleted_at IS NULL
		WHERE (g.user_id = ? AND w.user_id = ` + peer + `) OR (g.user_id = ` + peer + ` AND w.user_id = ?))`
}

const conversationColumns = `c.id, c.created_at,
	p.id AS peer_id, p.full_name AS peer_name, p.role AS peer_role, p.avatar_url AS peer_avatar_url,
	(p.deleted_at IS NOT NULL) AS peer_deleted,
	m.id AS last_id, m.sender_id AS last_sender_id, m.body AS last_body, (m.deleted_at IS NOT NULL) AS last_deleted,
	m.created_at AS last_created_at,
	(SELECT COUNT(*) FROM chat_messages AS x
		WHERE x.conversation_id = c.id AND x.sender_id <> ? AND x.deleted_at IS NULL
		AND x.id > COALESCE(mine.last_read_message_id, 0)) AS unread_count,
	COALESCE(theirs.last_read_message_id, 0) AS peer_last_read_message_id,
	(p.deleted_at IS NULL AND ` // the condition of sharedClass("p.id") follows, then the closing bracket

type ChatDBItf interface {
	// FindAccount returns an account that is not deleted, or nil
	FindAccount(ctx context.Context, userID uuid.UUID) (*AccountRow, error)
	// ShareClass reports whether a teacher and a parent have an active child in one class that is not archived, in either order
	ShareClass(ctx context.Context, a uuid.UUID, b uuid.UUID) (bool, error)
	// ListContacts returns the accounts of the other role that share an active class with the account, by name
	ListContacts(ctx context.Context, userID uuid.UUID, peerRole constants.Role, limit int, offset int) ([]ContactRow, int, error)
	// ContactLinks returns the shared classes with the child of each of the given contacts
	ContactLinks(ctx context.Context, userID uuid.UUID, peers []uuid.UUID) ([]ContactLinkRow, error)
	// ConversationLinks returns the conversations that exist between the account and each of the given people
	ConversationLinks(ctx context.Context, userID uuid.UUID, peers []uuid.UUID) ([]ConversationLinkRow, error)
	// GetOrCreateConversation returns the conversation of a pair of accounts, low being the smaller id, and whether this call made it
	GetOrCreateConversation(ctx context.Context, low uuid.UUID, high uuid.UUID, now time.Time) (*entity.ChatConversation, bool, error)
	// FindConversation returns nil when the conversation does not exist
	FindConversation(ctx context.Context, conversationID uuid.UUID) (*entity.ChatConversation, error)
	// FindConversationFor returns the conversation as the account sees it, with or without a message, or nil when it does not
	// exist or the account is not in it
	FindConversationFor(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID) (*ConversationRow, error)
	// ListConversations returns the conversations of the account that have a message, the newest message first
	ListConversations(ctx context.Context, userID uuid.UUID, limit int, offset int) ([]ConversationRow, int, error)
	// ListMessages returns up to limit messages in the order they were sent. With after it returns the ones that follow it,
	// otherwise the latest ones before the message called before. The flag tells whether more exist in that direction.
	ListMessages(ctx context.Context, conversationID uuid.UUID, after *int64, before *int64, limit int) ([]entity.ChatMessage, bool, error)
	// CreateMessage stores a message under a lock on its conversation, so the ids of one conversation are in the order the
	// messages commit. It returns ErrConversationGone when the conversation was deleted in the meantime.
	CreateMessage(ctx context.Context, message *entity.ChatMessage) error
	// FindMessage returns nil when the conversation has no such message
	FindMessage(ctx context.Context, conversationID uuid.UUID, messageID int64) (*entity.ChatMessage, error)
	// DeleteMessage empties a message of its sender and keeps the time of the first deletion
	DeleteMessage(ctx context.Context, conversationID uuid.UUID, messageID int64, senderID uuid.UUID, now time.Time) error
	// MarkRead moves the read mark of the account forward to the message, never back and never past the newest message,
	// and returns the mark and the number of unread messages from the other side
	MarkRead(ctx context.Context, conversationID uuid.UUID, userID uuid.UUID, messageID int64, now time.Time) (int64, int, error)
}

type ChatDB struct {
	db *gorm.DB
}

func NewChatDB(db *gorm.DB) ChatDBItf {
	return &ChatDB{db: db}
}

func (r *ChatDB) FindAccount(ctx context.Context, userID uuid.UUID) (*AccountRow, error) {
	var rows []AccountRow

	err := r.db.WithContext(ctx).
		Table("users").
		Select("id, role, full_name, avatar_url").
		Where("id = ? AND deleted_at IS NULL", userID).
		Limit(1).
		Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}

	return &rows[0], nil
}

func (r *ChatDB) ShareClass(ctx context.Context, a uuid.UUID, b uuid.UUID) (bool, error) {
	var shared bool

	err := r.db.WithContext(ctx).Raw("SELECT "+sharedClass("?"), a, b, b, a).Scan(&shared).Error

	return shared, err
}

func (r *ChatDB) ListContacts(
	ctx context.Context, userID uuid.UUID, peerRole constants.Role, limit int, offset int,
) ([]ContactRow, int, error) {
	filter := "u.deleted_at IS NULL AND u.role = ? AND " + sharedClass("u.id")

	var total int64

	err := r.db.WithContext(ctx).
		Raw("SELECT COUNT(*) FROM users AS u WHERE "+filter, peerRole, userID, userID).
		Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var rows []ContactRow

	err = r.db.WithContext(ctx).
		Raw(
			"SELECT u.id AS user_id, u.full_name, u.role, u.avatar_url FROM users AS u WHERE "+filter+
				" ORDER BY u.full_name ASC, u.id ASC LIMIT ? OFFSET ?",
			peerRole, userID, userID, limit, offset,
		).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(total), nil
}

func (r *ChatDB) ContactLinks(ctx context.Context, userID uuid.UUID, peers []uuid.UUID) ([]ContactLinkRow, error) {
	if len(peers) == 0 {
		return nil, nil
	}

	var rows []ContactLinkRow

	err := r.db.WithContext(ctx).
		Raw(`SELECT CASE WHEN g.user_id = ? THEN w.user_id ELSE g.user_id END AS peer_id,
				c.id AS class_id, c.name AS class_name, s.full_name AS child_name
			FROM class_teachers AS ct
			JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL
			JOIN classes AS c ON c.id = ct.class_id AND c.archived_at IS NULL
			JOIN students AS s ON s.class_id = c.id AND s.deleted_at IS NULL
			JOIN walis AS w ON w.id = s.wali_id AND w.deleted_at IS NULL
			WHERE (g.user_id = ? AND w.user_id IN ?) OR (w.user_id = ? AND g.user_id IN ?)
			ORDER BY c.name ASC, s.full_name ASC, c.id ASC, s.id ASC`,
			userID, userID, peers, userID, peers,
		).
		Scan(&rows).Error

	return rows, err
}

func (r *ChatDB) ConversationLinks(ctx context.Context, userID uuid.UUID, peers []uuid.UUID) ([]ConversationLinkRow, error) {
	if len(peers) == 0 {
		return nil, nil
	}

	var rows []ConversationLinkRow

	err := r.db.WithContext(ctx).
		Raw(`SELECT id, CASE WHEN user_low_id = ? THEN user_high_id ELSE user_low_id END AS peer_id
			FROM chat_conversations
			WHERE (user_low_id = ? AND user_high_id IN ?) OR (user_high_id = ? AND user_low_id IN ?)`,
			userID, userID, peers, userID, peers,
		).
		Scan(&rows).Error

	return rows, err
}

func (r *ChatDB) GetOrCreateConversation(
	ctx context.Context, low uuid.UUID, high uuid.UUID, now time.Time,
) (*entity.ChatConversation, bool, error) {
	conversation := entity.ChatConversation{ID: uuid.New(), UserLowID: low, UserHighID: high, CreatedAt: now}

	// the unique key of the pair settles two requests that start the same conversation at once, the second one reads the first
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&conversation)
	if res.Error != nil {
		return nil, false, res.Error
	}

	if res.RowsAffected > 0 {
		return &conversation, true, nil
	}

	var existing entity.ChatConversation

	err := r.db.WithContext(ctx).Where("user_low_id = ? AND user_high_id = ?", low, high).Take(&existing).Error
	if err != nil {
		return nil, false, err
	}

	return &existing, false, nil
}

func (r *ChatDB) FindConversation(ctx context.Context, conversationID uuid.UUID) (*entity.ChatConversation, error) {
	var rows []entity.ChatConversation

	err := r.db.WithContext(ctx).Where("id = ?", conversationID).Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}

	return &rows[0], nil
}

// conversationQuery selects the conversations of the account with the columns of ConversationRow. A conversation without a
// message has no last message, so it is joined with a left join and its last columns are empty.
func (r *ChatDB) conversationQuery(ctx context.Context, userID uuid.UUID, tail string, extra ...any) *gorm.DB {
	args := []any{userID, userID, userID, userID, userID, userID, userID}
	args = append(args, extra...)

	return r.db.WithContext(ctx).Raw(
		"SELECT "+conversationColumns+sharedClass("p.id")+`) AS can_send
		FROM chat_conversations AS c
		JOIN users AS p ON p.id = CASE WHEN c.user_low_id = ? THEN c.user_high_id ELSE c.user_low_id END
		LEFT JOIN LATERAL (
			SELECT * FROM chat_messages AS lm WHERE lm.conversation_id = c.id ORDER BY lm.id DESC LIMIT 1
		) AS m ON TRUE
		LEFT JOIN chat_reads AS mine ON mine.conversation_id = c.id AND mine.user_id = ?
		LEFT JOIN chat_reads AS theirs ON theirs.conversation_id = c.id AND theirs.user_id = p.id
		WHERE (c.user_low_id = ? OR c.user_high_id = ?) `+tail,
		args...,
	)
}

func (r *ChatDB) FindConversationFor(ctx context.Context, userID uuid.UUID, conversationID uuid.UUID) (*ConversationRow, error) {
	var rows []ConversationRow

	err := r.conversationQuery(ctx, userID, "AND c.id = ? LIMIT 1", conversationID).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}

	return &rows[0], nil
}

func (r *ChatDB) ListConversations(ctx context.Context, userID uuid.UUID, limit int, offset int) ([]ConversationRow, int, error) {
	var total int64

	err := r.db.WithContext(ctx).
		Raw(`SELECT COUNT(*) FROM chat_conversations AS c
			WHERE (c.user_low_id = ? OR c.user_high_id = ?)
			AND EXISTS (SELECT 1 FROM chat_messages AS m WHERE m.conversation_id = c.id)`, userID, userID).
		Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var rows []ConversationRow

	err = r.conversationQuery(
		ctx, userID, "AND m.id IS NOT NULL ORDER BY m.id DESC LIMIT ? OFFSET ?", limit, offset,
	).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(total), nil
}

func (r *ChatDB) ListMessages(
	ctx context.Context, conversationID uuid.UUID, after *int64, before *int64, limit int,
) ([]entity.ChatMessage, bool, error) {
	query := r.db.WithContext(ctx).Where("conversation_id = ?", conversationID)

	if after != nil {
		query = query.Where("id > ?", *after).Order("id ASC")
	} else {
		if before != nil {
			query = query.Where("id < ?", *before)
		}

		query = query.Order("id DESC")
	}

	var messages []entity.ChatMessage

	// one more than asked for tells whether anything is left
	err := query.Limit(limit + 1).Find(&messages).Error
	if err != nil {
		return nil, false, err
	}

	more := len(messages) > limit
	if more {
		messages = messages[:limit]
	}

	// the latest messages were read newest first, the answer is always in the order they were sent
	if after == nil {
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}

	return messages, more, nil
}

func (r *ChatDB) CreateMessage(ctx context.Context, message *entity.ChatMessage) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// two messages of one conversation are written one after the other, so the one with the smaller id has committed
		// when the next one gets its id, and a client that holds a cursor never skips a message that commits late
		locked := tx.Exec("SELECT 1 FROM chat_conversations WHERE id = ? FOR NO KEY UPDATE", message.ConversationID)
		if locked.Error != nil {
			return locked.Error
		}

		if locked.RowsAffected == 0 {
			return ErrConversationGone
		}

		return tx.Create(message).Error
	})
}

func (r *ChatDB) FindMessage(ctx context.Context, conversationID uuid.UUID, messageID int64) (*entity.ChatMessage, error) {
	var rows []entity.ChatMessage

	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND id = ?", conversationID, messageID).
		Limit(1).
		Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}

	return &rows[0], nil
}

func (r *ChatDB) DeleteMessage(
	ctx context.Context, conversationID uuid.UUID, messageID int64, senderID uuid.UUID, now time.Time,
) error {
	return r.db.WithContext(ctx).Exec(
		"UPDATE chat_messages SET body = '', deleted_at = COALESCE(deleted_at, ?) WHERE conversation_id = ? AND id = ? AND sender_id = ?",
		now, conversationID, messageID, senderID,
	).Error
}

func (r *ChatDB) MarkRead(
	ctx context.Context, conversationID uuid.UUID, userID uuid.UUID, messageID int64, now time.Time,
) (int64, int, error) {
	var (
		mark   int64
		unread int64
	)

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Raw(`INSERT INTO chat_reads (conversation_id, user_id, last_read_message_id, updated_at)
			VALUES (?, ?, LEAST(?::bigint, COALESCE((SELECT MAX(id) FROM chat_messages WHERE conversation_id = ?), 0)), ?)
			ON CONFLICT (conversation_id, user_id) DO UPDATE
			SET last_read_message_id = GREATEST(chat_reads.last_read_message_id, EXCLUDED.last_read_message_id),
				updated_at = EXCLUDED.updated_at
			RETURNING last_read_message_id`,
			conversationID, userID, messageID, conversationID, now,
		).Scan(&mark).Error
		if err != nil {
			return err
		}

		return tx.Raw(
			"SELECT COUNT(*) FROM chat_messages WHERE conversation_id = ? AND sender_id <> ? AND deleted_at IS NULL AND id > ?",
			conversationID, userID, mark,
		).Scan(&unread).Error
	})

	return mark, int(unread), err
}
