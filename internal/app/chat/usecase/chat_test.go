package usecase

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
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
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
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
	messages      map[int64]*repository.MessageRow
	createErr     error
	created       []*entity.ChatMessage
	contactRows   []repository.ContactRow
	contactLinks  []repository.ContactLinkRow

	// memberships holds what the signed in account has in each class, a class that is missing means it is no member
	memberships map[uuid.UUID]*repository.ClassMembership
	// listedSince and markedSince record the join time that the use case passed down
	listedSince time.Time
	markedSince time.Time
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
		if conversation.UserLowID != nil && *conversation.UserLowID == low && *conversation.UserHighID == high {
			return conversation, false, nil
		}
	}

	conversation := &entity.ChatConversation{ID: uuid.New(), Kind: constants.ChatKindDirect, UserLowID: &low, UserHighID: &high, CreatedAt: now}
	f.conversations[conversation.ID] = conversation

	return conversation, true, nil
}

func (f *fakeRepo) MemberSince(_ context.Context, _ uuid.UUID, classID uuid.UUID) (*repository.ClassMembership, error) {
	return f.memberships[classID], nil
}

func (f *fakeRepo) FindClassRoom(_ context.Context, classID uuid.UUID) (*entity.ChatConversation, error) {
	for _, conversation := range f.conversations {
		if conversation.ClassID != nil && *conversation.ClassID == classID {
			return conversation, nil
		}
	}

	return nil, nil
}

func (f *fakeRepo) GetOrCreateClassRoom(ctx context.Context, classID uuid.UUID, now time.Time) (*entity.ChatConversation, bool, error) {
	existing, _ := f.FindClassRoom(ctx, classID)
	if existing != nil {
		return existing, false, nil
	}

	room := &entity.ChatConversation{ID: uuid.New(), Kind: constants.ChatKindClass, ClassID: &classID, CreatedAt: now}
	f.conversations[room.ID] = room

	return room, true, nil
}

func (f *fakeRepo) ListMessages(
	_ context.Context, _ uuid.UUID, since time.Time, _ *int64, _ *int64, _ int,
) ([]repository.MessageRow, bool, error) {
	f.listedSince = since

	return nil, false, nil
}

func (f *fakeRepo) MarkRead(_ context.Context, _ uuid.UUID, _ uuid.UUID, since time.Time, _ int64, _ time.Time) (int64, int, error) {
	f.markedSince = since

	return 0, 0, nil
}

func (f *fakeRepo) FindConversation(_ context.Context, id uuid.UUID) (*entity.ChatConversation, error) {
	return f.conversations[id], nil
}

func (f *fakeRepo) FindConversationFor(_ context.Context, _ uuid.UUID, id uuid.UUID) (*repository.ConversationRow, error) {
	if view, found := f.views[id]; found {
		return view, nil
	}

	return &repository.ConversationRow{
		ID: id, Kind: constants.ChatKindDirect, CanSend: true,
		PeerID: ptr(uuid.New()), PeerName: ptr("Siti"), PeerRole: ptr(constants.RoleWali), PeerDeleted: ptr(false),
	}, nil
}

func (f *fakeRepo) CreateMessage(_ context.Context, message *entity.ChatMessage) error {
	if f.createErr != nil {
		return f.createErr
	}

	message.ID = int64(len(f.created) + 1)
	f.created = append(f.created, message)

	return nil
}

func (f *fakeRepo) FindMessage(_ context.Context, conversationID uuid.UUID, id int64) (*repository.MessageRow, error) {
	if message, found := f.messages[id]; found {
		return message, nil
	}

	// a message that this test wrote through CreateMessage comes back with the name of its sender
	for _, created := range f.created {
		if created.ID == id {
			return &repository.MessageRow{
				ID: created.ID, ConversationID: conversationID, SenderID: created.SenderID, Body: created.Body,
				CreatedAt: created.CreatedAt, SenderName: "Bu Ani", SenderRole: constants.RoleGuru,
				MediaKey: created.MediaKey, MediaKind: created.MediaKind, MediaType: created.MediaType, MediaSize: created.MediaSize,
			}, nil
		}
	}

	return nil, nil
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

// fakeStorage is the private bucket in memory. It records what the use case stored, copied and removed.
type fakeStorage struct {
	s3.Disabled

	off      bool
	objects  map[string]*s3.Object
	uploaded []string
	copied   [][2]string
	deleted  []string
	signed   []string
	signErr  error
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: map[string]*s3.Object{}}
}

func (f *fakeStorage) Enabled() bool { return !f.off }

func (f *fakeStorage) Upload(_ context.Context, key string, contentType string, data []byte) (string, error) {
	f.objects[key] = &s3.Object{Size: int64(len(data)), ContentType: contentType}
	f.uploaded = append(f.uploaded, key)

	return "", nil
}

func (f *fakeStorage) PresignUpload(_ context.Context, key string, _ string, _ int64, ttl time.Duration) (string, error) {
	return "https://upload.example.test/" + key + "?ttl=" + ttl.String(), nil
}

func (f *fakeStorage) Stat(_ context.Context, key string) (*s3.Object, error) {
	return f.objects[key], nil
}

func (f *fakeStorage) Copy(_ context.Context, source string, key string, contentType string) error {
	f.objects[key] = &s3.Object{Size: f.objects[source].Size, ContentType: contentType}
	f.copied = append(f.copied, [2]string{source, key})

	return nil
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	delete(f.objects, key)

	f.deleted = append(f.deleted, key)

	return nil
}

func (f *fakeStorage) PresignRead(_ context.Context, key string, ttl time.Duration) (string, error) {
	if f.signErr != nil {
		return "", f.signErr
	}

	f.signed = append(f.signed, key)

	return "https://read.example.test/" + key + "?ttl=" + ttl.String(), nil
}

func testConfig() *env.Env {
	return &env.Env{ChatPhotoMaxMB: 4, ChatVideoMaxMB: 25, ChatMediaURLSeconds: 3600, VideoUploadURLSeconds: 900}
}

func newUseCase(repo *fakeRepo, storage ...*fakeStorage) *ChatUseCase {
	if repo.conversations == nil {
		repo.conversations = map[uuid.UUID]*entity.ChatConversation{}
	}

	bucket := newFakeStorage()
	if len(storage) > 0 {
		bucket = storage[0]
	}

	return &ChatUseCase{
		repo: repo, storage: bucket, cfg: testConfig(),
		now: func() time.Time { return time.Date(2026, 10, 20, 9, 0, 0, 123456789, time.UTC) },
	}
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

	_, _, err := newUseCase(&fakeRepo{}).StartConversation(context.Background(), me, constants.RoleGuru, dto.StartChatRequest{UserID: &me})

	requireAppError(t, err, apperror.Validation(nil))
}

func TestStartConversationRefusesAnAdmin(t *testing.T) {
	_, _, err := newUseCase(&fakeRepo{}).StartConversation(
		context.Background(), uuid.New(), constants.RoleAdmin, dto.StartChatRequest{UserID: ptr(uuid.New())},
	)

	requireAppError(t, err, apperror.ErrForbidden)
}

func TestStartConversationGivesOneAnswerForAnUnknownAndAWrongRolePerson(t *testing.T) {
	peer := uuid.New()

	// an id that belongs to no active account
	_, _, err := newUseCase(&fakeRepo{}).StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: &peer},
	)
	requireAppError(t, err, apperror.ErrChatContactNotAllowed)

	// a teacher asking for another teacher
	repo := &fakeRepo{account: &repository.AccountRow{ID: peer, Role: constants.RoleGuru}, shared: true}

	_, _, err = newUseCase(repo).StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: &peer},
	)
	requireAppError(t, err, apperror.ErrChatContactNotAllowed)
}

func TestStartConversationNeedsASharedClassForANewOne(t *testing.T) {
	peer := uuid.New()
	repo := &fakeRepo{account: &repository.AccountRow{ID: peer, Role: constants.RoleWali}}

	_, _, err := newUseCase(repo).StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: &peer},
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
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: &peer},
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

	first, created, err := useCase.StartConversation(context.Background(), me, constants.RoleGuru, dto.StartChatRequest{UserID: &peer})
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := useCase.StartConversation(context.Background(), me, constants.RoleGuru, dto.StartChatRequest{UserID: &peer})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
	require.Len(t, repo.conversations, 1)

	stored := repo.conversations[first.ID]
	require.Negative(t, strings.Compare(stored.UserLowID.String(), stored.UserHighID.String()))
	require.Equal(t, constants.ChatKindDirect, first.Kind)
	require.NotNil(t, first.Peer)
	require.Nil(t, first.Class)
}

func conversationOf(low uuid.UUID, high uuid.UUID) (*entity.ChatConversation, uuid.UUID) {
	id := uuid.New()

	return &entity.ChatConversation{ID: id, Kind: constants.ChatKindDirect, UserLowID: &low, UserHighID: &high}, id
}

func ptr[T any](value T) *T {
	return &value
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
	require.Equal(t, "Bu Ani", res.Sender.FullName)
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
		messages: map[int64]*repository.MessageRow{
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
		ID: uuid.New(), Kind: constants.ChatKindDirect, PeerID: ptr(uuid.New()), PeerName: ptr("Siti"),
		PeerRole: ptr(constants.RoleWali), PeerAvatarURL: &avatar, PeerDeleted: ptr(false),
		UnreadCount: 2, PeerLastReadMessageID: 7, CanSend: true,
	}

	active := newUseCase(&fakeRepo{}).conversationResponse(context.Background(), row)
	require.Equal(t, "Siti", active.Peer.FullName)
	require.Equal(t, &avatar, active.Peer.AvatarURL)
	require.False(t, active.Peer.Deleted)
	require.Nil(t, active.LastMessage)
	require.Equal(t, 2, active.UnreadCount)
	require.Equal(t, int64(7), active.PeerLastReadID)

	id, sender, body, deleted, at := int64(9), uuid.New(), "see you", false, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	row.LastID, row.LastSenderID, row.LastBody, row.LastDeleted, row.LastCreatedAt = &id, &sender, &body, &deleted, &at
	row.LastSenderName, row.LastSenderRole, row.LastSenderDeleted = ptr("Bu Ani"), ptr(constants.RoleGuru), ptr(true)
	row.PeerDeleted = ptr(true)

	gone := newUseCase(&fakeRepo{}).conversationResponse(context.Background(), row)
	require.Equal(t, DeletedAccountName, gone.Peer.FullName)
	require.Nil(t, gone.Peer.AvatarURL)
	require.True(t, gone.Peer.Deleted)
	require.NotNil(t, gone.LastMessage)
	require.Equal(t, int64(9), gone.LastMessage.ID)
	require.Equal(t, "see you", gone.LastMessage.Body)
	require.Equal(t, row.ID, gone.LastMessage.ConversationID)
	require.Equal(t, DeletedAccountName, gone.LastMessage.Sender.FullName)
}

func TestConversationResponseOfARoomHasTheClassAndNoPeer(t *testing.T) {
	classID := uuid.New()
	row := &repository.ConversationRow{ID: uuid.New(), Kind: constants.ChatKindClass, ClassID: &classID, ClassName: ptr("Kelas A"), CanSend: true}

	res := newUseCase(&fakeRepo{}).conversationResponse(context.Background(), row)

	require.Equal(t, constants.ChatKindClass, res.Kind)
	require.Nil(t, res.Peer)
	require.Equal(t, &dto.ChatContactClass{ID: classID, Name: "Kelas A"}, res.Class)
}

func TestStartConversationNeedsExactlyOneOfAPersonAndAClass(t *testing.T) {
	useCase := newUseCase(&fakeRepo{})

	_, _, err := useCase.StartConversation(context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{})
	requireAppError(t, err, apperror.Validation(nil))

	_, _, err = useCase.StartConversation(
		context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{UserID: ptr(uuid.New()), ClassID: ptr(uuid.New())},
	)
	requireAppError(t, err, apperror.Validation(nil))
}

func TestOpeningARoomNeedsMembershipAndGivesOneAnswerForAnUnknownClass(t *testing.T) {
	classID := uuid.New()
	repo := &fakeRepo{}

	_, _, err := newUseCase(repo).StartConversation(
		context.Background(), uuid.New(), constants.RoleWali, dto.StartChatRequest{ClassID: &classID},
	)

	requireAppError(t, err, apperror.ErrChatContactNotAllowed)
	require.Empty(t, repo.conversations)
}

func TestOpeningARoomCreatesItOnceForEveryMember(t *testing.T) {
	classID := uuid.New()
	repo := &fakeRepo{memberships: map[uuid.UUID]*repository.ClassMembership{classID: {ClassName: "Kelas A", Since: time.Now()}}}
	useCase := newUseCase(repo)

	first, created, err := useCase.StartConversation(context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{ClassID: &classID})
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := useCase.StartConversation(context.Background(), uuid.New(), constants.RoleWali, dto.StartChatRequest{ClassID: &classID})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
	require.Len(t, repo.conversations, 1)
}

func TestAnArchivedClassOnlyReturnsARoomThatAlreadyExists(t *testing.T) {
	classID := uuid.New()
	repo := &fakeRepo{memberships: map[uuid.UUID]*repository.ClassMembership{classID: {Archived: true, Since: time.Now()}}}
	useCase := newUseCase(repo)

	_, _, err := useCase.StartConversation(context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{ClassID: &classID})
	requireAppError(t, err, apperror.ErrChatContactNotAllowed)
	require.Empty(t, repo.conversations)

	room := &entity.ChatConversation{ID: uuid.New(), Kind: constants.ChatKindClass, ClassID: &classID}
	repo.conversations = map[uuid.UUID]*entity.ChatConversation{room.ID: room}
	repo.views = map[uuid.UUID]*repository.ConversationRow{room.ID: {ID: room.ID, Kind: constants.ChatKindClass, CanSend: false}}

	res, created, err := useCase.StartConversation(context.Background(), uuid.New(), constants.RoleGuru, dto.StartChatRequest{ClassID: &classID})
	require.NoError(t, err)
	require.False(t, created)
	require.False(t, res.CanSend)
}

func TestARoomShowsAMemberOnlyTheMessagesFromTheirJoinTime(t *testing.T) {
	classID := uuid.New()
	joined := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	room := &entity.ChatConversation{ID: uuid.New(), Kind: constants.ChatKindClass, ClassID: &classID}
	repo := &fakeRepo{
		conversations: map[uuid.UUID]*entity.ChatConversation{room.ID: room},
		memberships:   map[uuid.UUID]*repository.ClassMembership{classID: {Since: joined}},
	}
	useCase := newUseCase(repo)
	me := uuid.New()

	_, err := useCase.ListMessages(context.Background(), me, room.ID, nil, nil, 10)
	require.NoError(t, err)
	require.Equal(t, joined, repo.listedSince)

	_, err = useCase.MarkRead(context.Background(), me, room.ID, dto.MarkChatReadRequest{MessageID: 5})
	require.NoError(t, err)
	require.Equal(t, joined, repo.markedSince)
}

func TestARoomIsNotFoundForAnAccountThatLeftTheClass(t *testing.T) {
	classID := uuid.New()
	room := &entity.ChatConversation{ID: uuid.New(), Kind: constants.ChatKindClass, ClassID: &classID}
	repo := &fakeRepo{conversations: map[uuid.UUID]*entity.ChatConversation{room.ID: room}}
	useCase := newUseCase(repo)
	ctx := context.Background()

	_, err := useCase.ListMessages(ctx, uuid.New(), room.ID, nil, nil, 10)
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	_, err = useCase.SendMessage(ctx, uuid.New(), room.ID, dto.SendChatMessageRequest{Body: "hi"})
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	_, err = useCase.DeleteMessage(ctx, uuid.New(), room.ID, 1)
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	_, err = useCase.MarkRead(ctx, uuid.New(), room.ID, dto.MarkChatReadRequest{MessageID: 1})
	requireAppError(t, err, apperror.ErrChatConversationNotFound)
}

// pngOf is a small valid image, which is all the inspection of a photo needs
func pngOf(t *testing.T) []byte {
	t.Helper()

	var out bytes.Buffer

	require.NoError(t, png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2))))

	return out.Bytes()
}

func openConversation(me uuid.UUID) (*fakeRepo, uuid.UUID) {
	conversation, id := conversationOf(me, uuid.New())

	return &fakeRepo{conversations: map[uuid.UUID]*entity.ChatConversation{id: conversation}}, id
}

func TestAPhotoWaitsUnderPendingForTheSenderAndTheConversation(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)
	bucket := newFakeStorage()

	res, err := newUseCase(repo, bucket).UploadPhoto(context.Background(), me, id, pngOf(t))

	require.NoError(t, err)
	require.Equal(t, "photo", res.Kind)
	require.Equal(t, "image/png", res.ContentType)
	require.True(t, strings.HasPrefix(res.MediaKey, "pending/chat/"+id.String()+"/"+me.String()+"/"), res.MediaKey)
	require.True(t, strings.HasSuffix(res.MediaKey, ".png"))
	require.Equal(t, []string{res.MediaKey}, bucket.uploaded)
}

func TestAPhotoIsRefusedWhenItIsNotAnImageOrTooLargeOrNobodyCanUseIt(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)
	ctx := context.Background()

	_, err := newUseCase(repo).UploadPhoto(ctx, me, id, []byte("not an image at all"))
	requireAppError(t, err, apperror.ErrInvalidImage)

	big := newUseCase(repo)
	big.cfg.ChatPhotoMaxMB = 1

	_, err = big.UploadPhoto(ctx, me, id, append(pngOf(t), make([]byte, 2*bytesPerMB)...))
	requireAppError(t, err, apperror.ErrFileTooLarge)

	off := newFakeStorage()
	off.off = true

	_, err = newUseCase(repo, off).UploadPhoto(ctx, me, id, pngOf(t))
	requireAppError(t, err, apperror.ErrStorageDisabled)

	_, err = newUseCase(repo).UploadPhoto(ctx, uuid.New(), id, pngOf(t))
	requireAppError(t, err, apperror.ErrChatConversationNotFound)

	closed := &fakeRepo{
		conversations: repo.conversations,
		views:         map[uuid.UUID]*repository.ConversationRow{id: {ID: id, Kind: constants.ChatKindDirect, CanSend: false}},
	}

	_, err = newUseCase(closed).UploadPhoto(ctx, me, id, pngOf(t))
	requireAppError(t, err, apperror.ErrChatSendingClosed)
}

func TestAVideoUploadAddressIsCappedAndKeyedForTheSender(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)
	useCase := newUseCase(repo)
	ctx := context.Background()

	res, err := useCase.CreateVideoUpload(ctx, me, id, dto.ChatVideoUploadRequest{ContentType: "video/mp4", SizeBytes: 5 * bytesPerMB})

	require.NoError(t, err)
	require.Equal(t, "PUT", res.Method)
	require.Equal(t, map[string]string{"Content-Type": "video/mp4"}, res.Headers)
	require.True(t, strings.HasPrefix(res.MediaKey, "pending/chat/"+id.String()+"/"+me.String()+"/"))
	require.True(t, strings.HasSuffix(res.MediaKey, ".mp4"))
	require.Contains(t, res.UploadURL, res.MediaKey)
	require.Equal(t, time.Date(2026, 10, 20, 9, 15, 0, 123456789, time.UTC), res.ExpiresAt)

	_, err = useCase.CreateVideoUpload(ctx, me, id, dto.ChatVideoUploadRequest{ContentType: "video/mp4", SizeBytes: 25*bytesPerMB + 1})
	requireAppError(t, err, apperror.Validation(nil))
}

func stagedUpload(me uuid.UUID, id uuid.UUID, name string, object *s3.Object) (string, *fakeStorage) {
	bucket := newFakeStorage()
	key := pendingKeyPrefix(id, me) + name

	if object != nil {
		bucket.objects[key] = object
	}

	return key, bucket
}

func TestAMessageCarriesAPhotoThatWasUploadedForIt(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)
	key, bucket := stagedUpload(me, id, "a.png", &s3.Object{Size: 1234, ContentType: "image/png"})

	res, err := newUseCase(repo, bucket).SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{MediaKey: key})

	require.NoError(t, err)
	require.Empty(t, res.Body, "a photo can go without text")
	require.NotNil(t, res.Media)
	require.Equal(t, "photo", res.Media.Kind)
	require.Equal(t, "image/png", res.Media.ContentType)
	require.EqualValues(t, 1234, res.Media.SizeBytes)
	require.Equal(t, time.Date(2026, 10, 20, 10, 0, 0, 123456789, time.UTC), res.Media.ExpiresAt)

	require.Len(t, bucket.copied, 1)
	require.Equal(t, key, bucket.copied[0][0])
	require.True(t, strings.HasPrefix(bucket.copied[0][1], "chat/"+id.String()+"/"), bucket.copied[0][1])
	require.Equal(t, []string{key}, bucket.deleted, "the staged file goes once the message is stored")
	require.Contains(t, res.Media.URL, bucket.copied[0][1], "the address reads the permanent file")
	require.Len(t, repo.created, 1)
	require.Equal(t, bucket.copied[0][1], *repo.created[0].MediaKey)
}

func TestAVideoIsCheckedAgainstItsOwnLimit(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)

	key, bucket := stagedUpload(me, id, "a.mp4", &s3.Object{Size: 20 * bytesPerMB, ContentType: "video/mp4"})

	res, err := newUseCase(repo, bucket).SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{Body: "look", MediaKey: key})
	require.NoError(t, err)
	require.Equal(t, "video", res.Media.Kind)
	require.Equal(t, "look", res.Body)

	// the same size is too much for a photo
	tooBig, bucket := stagedUpload(me, id, "b.png", &s3.Object{Size: 20 * bytesPerMB, ContentType: "image/png"})

	_, err = newUseCase(repo, bucket).SendMessage(context.Background(), me, id, dto.SendChatMessageRequest{MediaKey: tooBig})
	requireAppError(t, err, apperror.Validation(nil))
	require.Empty(t, bucket.copied)
}

func TestAMessageRefusesAKeyThatIsNotAFinishedUploadOfTheSenderForThisConversation(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)
	ctx := context.Background()

	_, bucket := stagedUpload(me, id, "a.png", nil)
	useCase := newUseCase(repo, bucket)

	// an upload that never finished
	_, err := useCase.SendMessage(ctx, me, id, dto.SendChatMessageRequest{MediaKey: pendingKeyPrefix(id, me) + "a.png"})
	requireAppError(t, err, apperror.Validation(nil))

	// the key of another account, of another conversation, and a key that points anywhere else
	for _, key := range []string{
		pendingKeyPrefix(id, uuid.New()) + "a.png",
		pendingKeyPrefix(uuid.New(), me) + "a.png",
		"chat/" + id.String() + "/a.png",
		"avatars/" + me.String() + "/a.png",
		"../" + pendingKeyPrefix(id, me) + "a.png",
		pendingKeyPrefix(id, me) + "../" + id.String() + "/a.png",
		pendingKeyPrefix(id, me),
	} {
		bucket.objects[key] = &s3.Object{Size: 10, ContentType: "image/png"}

		_, err = useCase.SendMessage(ctx, me, id, dto.SendChatMessageRequest{MediaKey: key})
		requireAppError(t, err, apperror.Validation(nil))
	}

	// a file of a type that the chat does not carry
	odd, bucket := stagedUpload(me, id, "a.pdf", &s3.Object{Size: 10, ContentType: "application/pdf"})

	_, err = newUseCase(repo, bucket).SendMessage(ctx, me, id, dto.SendChatMessageRequest{MediaKey: odd})
	requireAppError(t, err, apperror.Validation(nil))

	require.Empty(t, repo.created)
	require.Empty(t, bucket.copied)
}

func TestAMessageNeedsTextOrMediaAndCleansUpWhenItCannotBeStored(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)
	ctx := context.Background()

	_, err := newUseCase(repo).SendMessage(ctx, me, id, dto.SendChatMessageRequest{})
	requireAppError(t, err, apperror.Validation(nil))

	key, bucket := stagedUpload(me, id, "a.png", &s3.Object{Size: 10, ContentType: "image/png"})
	repo.createErr = errors.New("connection reset")

	_, err = newUseCase(repo, bucket).SendMessage(ctx, me, id, dto.SendChatMessageRequest{MediaKey: key})
	requireAppError(t, err, apperror.Internal(nil))
	require.Len(t, bucket.deleted, 1, "the permanent copy is removed again")
	require.Equal(t, bucket.copied[0][1], bucket.deleted[0])
	require.Contains(t, bucket.objects, key, "the staged file stays so the app can try again")
}

func TestAMessageWithMediaNeedsTheBucketToBeOn(t *testing.T) {
	me := uuid.New()
	repo, id := openConversation(me)
	off := newFakeStorage()
	off.off = true

	_, err := newUseCase(repo, off).SendMessage(
		context.Background(), me, id, dto.SendChatMessageRequest{MediaKey: pendingKeyPrefix(id, me) + "a.png"},
	)

	requireAppError(t, err, apperror.ErrStorageDisabled)
}

func TestAReadAddressThatCannotBeSignedLeavesTheMessageInTheList(t *testing.T) {
	bucket := newFakeStorage()
	bucket.signErr = errors.New("no credentials")
	useCase := newUseCase(&fakeRepo{}, bucket)
	key, kind, contentType, size := "chat/a/b.png", "photo", "image/png", int64(10)

	media := useCase.mediaOf(context.Background(), &key, &kind, &contentType, &size)

	require.NotNil(t, media)
	require.Empty(t, media.URL)
	require.Equal(t, "photo", media.Kind)

	require.Nil(t, useCase.mediaOf(context.Background(), nil, nil, nil, nil), "a message without media has none")
}
