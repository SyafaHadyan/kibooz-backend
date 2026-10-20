package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/chat/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

// fakeRepo answers from memory. A method that a test does not set up panics on the nil interface, which shows that the
// code under test reached for something it should not.
type fakeRepo struct {
	repository.ChatDBItf

	account       *repository.AccountRow
	shared        bool
	conversations map[uuid.UUID]*entity.ChatConversation
	links         []repository.ConversationLinkRow
	views         map[uuid.UUID]*repository.ConversationRow
	messages      map[int64]*entity.ChatMessage
	createErr     error
	created       []*entity.ChatMessage
	contactRows   []repository.ContactRow
	contactLinks  []repository.ContactLinkRow
}

func (f *fakeRepo) FindAccount(context.Context, uuid.UUID) (*repository.AccountRow, error) {
	return f.account, nil
}

func (f *fakeRepo) ShareClass(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.shared, nil
}

func (f *fakeRepo) ConversationLinks(context.Context, uuid.UUID, []uuid.UUID) ([]repository.ConversationLinkRow, error) {
	return f.links, nil
}

func (f *fakeRepo) GetOrCreateConversation(
	_ context.Context, low uuid.UUID, high uuid.UUID, now time.Time,
) (*entity.ChatConversation, bool, error) {
	for _, conversation := range f.conversations {
		if conversation.UserLowID == low && conversation.UserHighID == high {
			return conversation, false, nil
		}
	}

	conversation := &entity.ChatConversation{ID: uuid.New(), UserLowID: low, UserHighID: high, CreatedAt: now}
	f.conversations[conversation.ID] = conversation

	return conversation, true, nil
}

func (f *fakeRepo) FindConversation(_ context.Context, id uuid.UUID) (*entity.ChatConversation, error) {
	return f.conversations[id], nil
}

func (f *fakeRepo) FindConversationFor(_ context.Context, _ uuid.UUID, id uuid.UUID) (*repository.ConversationRow, error) {
	if view, found := f.views[id]; found {
		return view, nil
	}

	return &repository.ConversationRow{ID: id, CanSend: true}, nil
}

func (f *fakeRepo) CreateMessage(_ context.Context, message *entity.ChatMessage) error {
	if f.createErr != nil {
		return f.createErr
	}

	message.ID = int64(len(f.created) + 1)
	f.created = append(f.created, message)

	return nil
}

func (f *fakeRepo) FindMessage(_ context.Context, _ uuid.UUID, id int64) (*entity.ChatMessage, error) {
	return f.messages[id], nil
}

func (f *fakeRepo) DeleteMessage(_ context.Context, _ uuid.UUID, id int64, _ uuid.UUID, now time.Time) error {
	message := f.messages[id]
	message.Body = ""
	message.DeletedAt = &now

	return nil
}

func (f *fakeRepo) ListContacts(context.Context, uuid.UUID, constants.Role, int, int) ([]repository.ContactRow, int, error) {
	return f.contactRows, len(f.contactRows), nil
}

func (f *fakeRepo) ContactLinks(context.Context, uuid.UUID, []uuid.UUID) ([]repository.ContactLinkRow, error) {
	return f.contactLinks, nil
}

func newUseCase(repo *fakeRepo) *ChatUseCase {
	if repo.conversations == nil {
		repo.conversations = map[uuid.UUID]*entity.ChatConversation{}
	}

	return &ChatUseCase{repo: repo, now: func() time.Time { return time.Date(2026, 10, 20, 9, 0, 0, 123456789, time.UTC) }}
}

func requireAppError(t *testing.T, err error, want *apperror.Error) {
	t.Helper()

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, want.Code, got.Code)
	require.Equal(t, want.Status, got.Status)
}

func TestOrderedPairPutsTheSmallerIDFirstInEitherOrder(t *testing.T) {
	small := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	large := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")

	low, high := orderedPair(small, large)
	require.Equal(t, small, low)
	require.Equal(t, large, high)

	low, high = orderedPair(large, small)
	require.Equal(t, small, low)
	require.Equal(t, large, high)
}

func TestPeerRoleIsTheOtherRoleAndAdminHasNone(t *testing.T) {
	role, ok := peerRole(constants.RoleGuru)
	require.True(t, ok)
	require.Equal(t, constants.RoleWali, role)

	role, ok = peerRole(constants.RoleWali)
	require.True(t, ok)
	require.Equal(t, constants.RoleGuru, role)

	_, ok = peerRole(constants.RoleAdmin)
	require.False(t, ok)
}

func TestStartConversationRefusesYourOwnAccount(t *testing.T) {
	me := uuid.New()

	_, _, err := newUseCase(&fakeRepo{}).StartConversation(context.Background(), me, constants.RoleGuru, dto.StartChatRequest{UserID: me})

	requireAppError(t, err, apperror.Validation(nil))
}

func TestStartConversationRefusesAnAdmin(t *testing.T) {
	_, _, err := newUseCase(&fakeRepo{}).StartConversation(
		context.Background(), uuid.New(), constants.RoleAdmin, dto.StartChatRequest{UserID: uuid.New()},
	)

	requireAppError(t, err, apperror.ErrForbidden)
}

func TestStartConversationGivesOneAnswerForAnUnknownAndAWrongRolePerson(t *testing.T) {
	peer := uuid.New()

	// an id that belongs to no active account
	_, _, err := newUseCase(&fakeRepo{}).StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: peer},
	)
	requireAppError(t, err, apperror.ErrChatContactNotAllowed)

	// a teacher asking for another teacher
	repo := &fakeRepo{account: &repository.AccountRow{ID: peer, Role: constants.RoleGuru}, shared: true}

	_, _, err = newUseCase(repo).StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: peer},
	)
	requireAppError(t, err, apperror.ErrChatContactNotAllowed)
}

func TestStartConversationNeedsASharedClassForANewOne(t *testing.T) {
	peer := uuid.New()
	repo := &fakeRepo{account: &repository.AccountRow{ID: peer, Role: constants.RoleWali}}

	_, _, err := newUseCase(repo).StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: peer},
	)

	requireAppError(t, err, apperror.ErrChatContactNotAllowed)
	require.Empty(t, repo.conversations)
}

func TestStartConversationReturnsTheOldOneOfPeopleWhoNoLongerShareAClass(t *testing.T) {
	peer, existing := uuid.New(), uuid.New()
	repo := &fakeRepo{
		account: &repository.AccountRow{ID: peer, Role: constants.RoleWali},
		links:   []repository.ConversationLinkRow{{ID: existing, PeerID: peer}},
		views:   map[uuid.UUID]*repository.ConversationRow{existing: {ID: existing, CanSend: false}},
	}

	res, created, err := newUseCase(repo).StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: peer},
	)

	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, existing, res.ID)
	require.False(t, res.CanSend)
}

func TestStartConversationCreatesOnceAndThenReturnsTheSameOne(t *testing.T) {
	me, peer := uuid.New(), uuid.New()
	repo := &fakeRepo{account: &repository.AccountRow{ID: peer, Role: constants.RoleWali}, shared: true}
	useCase := newUseCase(repo)

	first, created, err := useCase.StartConversation(context.Background(), me, constants.RoleGuru, dto.StartChatRequest{UserID: peer})
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := useCase.StartConversation(context.Background(), me, constants.RoleGuru, dto.StartChatRequest{UserID: peer})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
	require.Len(t, repo.conversations, 1)

	stored := repo.conversations[first.ID]
	require.Negative(t, strings.Compare(stored.UserLowID.String(), stored.UserHighID.String()))
}

func conversationOf(low uuid.UUID, high uuid.UUID) (*entity.ChatConversation, uuid.UUID) {
	id := uuid.New()

	return &entity.ChatConversation{ID: id, UserLowID: low, UserHighID: high}, id
}

func TestAnAccountOutsideAConversationGetsNotFoundOnEveryRoute(t *testing.T) {
	conversation, id := conversationOf(uuid.New(), uuid.New())
	repo := &fakeRepo{conversations: map[uuid.UUID]*entity.ChatConversation{id: conversation}}
	useCase := newUseCase(repo)
	outsider := uuid.New()
	ctx := context.Background()

	_, err := useCase.ListMessages(ctx, outsider, id, nil, nil, 10)
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	_, err = useCase.SendMessage(ctx, outsider, id, dto.SendChatMessageRequest{Body: "hi"})
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	_, err = useCase.DeleteMessage(ctx, outsider, id, 1)
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	_, err = useCase.MarkRead(ctx, outsider, id, dto.MarkChatReadRequest{MessageID: 1})
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	_, err = useCase.ListMessages(ctx, outsider, uuid.New(), nil, nil, 10)
	requireAppError(t, err, apperror.ErrChatConversationNotFound)
}

func TestListMessagesChecksTheCursorsAndTheLimit(t *testing.T) {
	me := uuid.New()
	conversation, id := conversationOf(me, uuid.New())
	useCase := newUseCase(&fakeRepo{conversations: map[uuid.UUID]*entity.ChatConversation{id: conversation}})
	one, minusOne, zero := int64(1), int64(-1), int64(0)

	tests := map[string]struct {
		after  *int64
		before *int64
		limit  int
	}{
		"after and before together": {after: &one, before: &one, limit: 10},
		"a negative after":          {after: &minusOne, limit: 10},
		"a before of zero":          {before: &zero, limit: 10},
		"a limit of zero":           {limit: 0},
		"a limit over the maximum":  {limit: 101},
	}

	for name, test := range tests {
		_, err := useCase.ListMessages(context.Background(), me, id, test.after, test.before, test.limit)

		requireAppError(t, err, apperror.Validation(nil))
		require.Error(t, err, name)
	}
}

func TestSendMessageTrimsTheTextAndStoresItForTheSender(t *testing.T) {
	me := uuid.New()
	conversation, id := conversationOf(me, uuid.New())
	repo := &fakeRepo{conversations: map[uuid.UUID]*entity.ChatConversation{id: conversation}}

	res, err := newUseCase(repo).SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{Body: "  Good morning\n "})

	require.NoError(t, err)
	require.Equal(t, "Good morning", res.Body)
	require.Equal(t, me, res.SenderID)
	require.Equal(t, id, res.ConversationID)
	require.False(t, res.Deleted)
	require.Equal(t, time.Date(2026, 10, 20, 9, 0, 0, 123456000, time.UTC), res.CreatedAt)
	require.Len(t, repo.created, 1)
}

func TestSendMessageRefusesBlankTextAndAClosedConversation(t *testing.T) {
	me := uuid.New()
	conversation, id := conversationOf(me, uuid.New())
	repo := &fakeRepo{
		conversations: map[uuid.UUID]*entity.ChatConversation{id: conversation},
		views:         map[uuid.UUID]*repository.ConversationRow{id: {ID: id, CanSend: false}},
	}
	useCase := newUseCase(repo)

	_, err := useCase.SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{Body: " \n\t "})
	requireAppError(t, err, apperror.Validation(nil))

	_, err = useCase.SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{Body: "hello"})
	requireAppError(t, err, apperror.ErrChatSendingClosed)
	require.Empty(t, repo.created)
}

func TestSendMessageReportsAConversationThatWasDeletedMeanwhile(t *testing.T) {
	me := uuid.New()
	conversation, id := conversationOf(me, uuid.New())
	repo := &fakeRepo{
		conversations: map[uuid.UUID]*entity.ChatConversation{id: conversation},
		createErr:     repository.ErrConversationGone,
	}

	_, err := newUseCase(repo).SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{Body: "hello"})
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	repo.createErr = errors.New("connection reset")

	_, err = newUseCase(repo).SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{Body: "hello"})
	requireAppError(t, err, apperror.Internal(nil))
}

func TestDeleteMessageIsForTheSenderAndLeavesAPlaceholder(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	conversation, id := conversationOf(me, other)
	repo := &fakeRepo{
		conversations: map[uuid.UUID]*entity.ChatConversation{id: conversation},
		messages: map[int64]*entity.ChatMessage{
			1: {ID: 1, ConversationID: id, SenderID: me, Body: "mine"},
			2: {ID: 2, ConversationID: id, SenderID: other, Body: "theirs"},
		},
	}
	useCase := newUseCase(repo)

	_, err := useCase.DeleteMessage(context.Background(), me, id, 2)
	requireAppError(t, err, apperror.ErrForbidden)
	require.Equal(t, "theirs", repo.messages[2].Body)

	_, err = useCase.DeleteMessage(context.Background(), me, id, 99)
	requireAppError(t, err, apperror.ErrChatMessageNotFound)

	res, err := useCase.DeleteMessage(context.Background(), me, id, 1)
	require.NoError(t, err)
	require.True(t, res.Deleted)
	require.Empty(t, res.Body)
}

func TestListContactsJoinsEachPersonWithTheirClassesChildrenAndConversation(t *testing.T) {
	me, parent, classA, classB, conversation := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &fakeRepo{
		contactRows: []repository.ContactRow{{UserID: parent, FullName: "Siti", Role: constants.RoleWali}},
		contactLinks: []repository.ContactLinkRow{
			{PeerID: parent, ClassID: classA, ClassName: "Kelas A", ChildName: "Rani"},
			{PeerID: parent, ClassID: classB, ClassName: "Kelas B", ChildName: "Rani"},
			{PeerID: parent, ClassID: classB, ClassName: "Kelas B", ChildName: "Dimas"},
			{PeerID: uuid.New(), ClassID: classA, ClassName: "Kelas A", ChildName: "Somebody else"},
		},
		links: []repository.ConversationLinkRow{{ID: conversation, PeerID: parent}},
	}

	res, err := newUseCase(repo).ListContacts(context.Background(), me, constants.RoleGuru, pagination.Params{Page: 1, Limit: 20})

	require.NoError(t, err)
	require.Equal(t, 1, res.Total)
	require.Len(t, res.Contacts, 1)

	contact := res.Contacts[0]
	require.Equal(t, parent, contact.ID)
	require.Equal(t, []dto.ChatContactClass{{ID: classA, Name: "Kelas A"}, {ID: classB, Name: "Kelas B"}}, contact.Classes)
	require.Equal(t, []string{"Rani", "Dimas"}, contact.Children)
	require.NotNil(t, contact.ConversationID)
	require.Equal(t, conversation, *contact.ConversationID)
}

func TestListContactsRefusesAnAdminAndShowsNoConversationYet(t *testing.T) {
	_, err := newUseCase(&fakeRepo{}).ListContacts(context.Background(), uuid.New(), constants.RoleAdmin, pagination.Params{Page: 1, Limit: 20})
	requireAppError(t, err, apperror.ErrForbidden)

	repo := &fakeRepo{contactRows: []repository.ContactRow{{UserID: uuid.New(), FullName: "Bu Ani", Role: constants.RoleGuru}}}

	res, err := newUseCase(repo).ListContacts(context.Background(), uuid.New(), constants.RoleWali, pagination.Params{Page: 1, Limit: 20})
	require.NoError(t, err)
	require.Nil(t, res.Contacts[0].ConversationID)
	require.Empty(t, res.Contacts[0].Classes)
	require.Empty(t, res.Contacts[0].Children)
}

func TestConversationResponseHidesADeletedPersonAndAMissingLastMessage(t *testing.T) {
	avatar := "https://cdn.example.test/avatars/a.png"
	row := &repository.ConversationRow{
		ID: uuid.New(), PeerID: uuid.New(), PeerName: "Siti", PeerRole: constants.RoleWali, PeerAvatarURL: &avatar,
		UnreadCount: 2, PeerLastReadMessageID: 7, CanSend: true,
	}

	active := conversationResponse(row)
	require.Equal(t, "Siti", active.Peer.FullName)
	require.Equal(t, &avatar, active.Peer.AvatarURL)
	require.False(t, active.Peer.Deleted)
	require.Nil(t, active.LastMessage)
	require.Equal(t, 2, active.UnreadCount)
	require.Equal(t, int64(7), active.PeerLastReadID)

	id, sender, body, deleted, at := int64(9), uuid.New(), "see you", false, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	row.LastID, row.LastSenderID, row.LastBody, row.LastDeleted, row.LastCreatedAt = &id, &sender, &body, &deleted, &at
	row.PeerDeleted = true

	gone := conversationResponse(row)
	require.Equal(t, DeletedAccountName, gone.Peer.FullName)
	require.Nil(t, gone.Peer.AvatarURL)
	require.True(t, gone.Peer.Deleted)
	require.NotNil(t, gone.LastMessage)
	require.Equal(t, int64(9), gone.LastMessage.ID)
	require.Equal(t, "see you", gone.LastMessage.Body)
	require.Equal(t, row.ID, gone.LastMessage.ConversationID)
}
