package usecase_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/devicetoken"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/jwt"
)

type fakeRepo struct {
	user        *entity.User
	rotateErrs  []error
	rotateCalls int

	// rotation replaces the default outcome of a swap, which is a session for user, and policy keeps the limits it was given
	rotation *repository.Rotation
	policy   repository.RotationPolicy

	// registered keeps the session that came with an account, and separateTokens counts the tokens stored on their own
	registered     *entity.RefreshToken
	separateTokens int
	createErr      error
}

func (f *fakeRepo) FindUserByEmail(context.Context, string) (*entity.User, error) { return f.user, nil }

func (f *fakeRepo) FindUserByID(context.Context, uuid.UUID) (*entity.User, error) { return f.user, nil }

func (f *fakeRepo) CreateGuru(_ context.Context, _ *entity.User, _ *entity.Guru, _ *entity.Class, session *entity.RefreshToken) error {
	if f.createErr != nil {
		return f.createErr
	}

	f.registered = session

	return nil
}

func (f *fakeRepo) CreateWali(
	_ context.Context, _ *entity.User, _ *entity.Wali, _ string, _ *entity.Student, session *entity.RefreshToken,
) (uuid.UUID, error) {
	if f.createErr != nil {
		return uuid.Nil, f.createErr
	}

	f.registered = session

	return uuid.Nil, nil
}

func (f *fakeRepo) CreateRefreshToken(context.Context, *entity.RefreshToken) error {
	f.separateTokens++

	return nil
}

func (f *fakeRepo) RotateRefreshToken(
	_ context.Context, _ string, _ time.Time, _ *entity.RefreshToken, policy repository.RotationPolicy,
) (repository.Rotation, error) {
	f.rotateCalls++
	f.policy = policy

	if len(f.rotateErrs) > 0 {
		err := f.rotateErrs[0]
		f.rotateErrs = f.rotateErrs[1:]

		if err != nil {
			return repository.Rotation{}, err
		}
	}

	if f.rotation != nil {
		return *f.rotation, nil
	}

	return repository.Rotation{User: f.user}, nil
}

func (f *fakeRepo) DeleteRefreshToken(context.Context, string) error { return nil }

// fakeCache follows the contract of the real cache, a missing key is never flagged as used
type fakeCache struct{ keys map[string]string }

func newFakeCache() *fakeCache { return &fakeCache{keys: map[string]string{}} }

func (f *fakeCache) Set(_ context.Context, key string, value string, _ time.Duration) error {
	f.keys[key] = value

	return nil
}

func (f *fakeCache) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := f.keys[key]

	return value, ok, nil
}

func (f *fakeCache) Del(_ context.Context, keys ...string) error {
	for _, key := range keys {
		delete(f.keys, key)
	}

	return nil
}

func (f *fakeCache) MarkUsed(_ context.Context, key string, _ time.Duration) (bool, error) {
	value, ok := f.keys[key]
	if !ok {
		return false, nil
	}

	f.keys[key] = "used"

	return value == "used", nil
}

func (f *fakeCache) Available() bool { return true }

func (f *fakeCache) Ping(context.Context) error { return nil }

type fakeJWT struct{}

func (fakeJWT) GenerateToken(uuid.UUID, constants.Role) (string, error) { return "access", nil }

func (fakeJWT) ValidateToken(string) (*jwt.Claims, error) { return nil, errors.New("unused") }

// brokenJWT cannot sign a token
type brokenJWT struct{ fakeJWT }

func (brokenJWT) GenerateToken(uuid.UUID, constants.Role) (string, error) {
	return "", errors.New("no signing key")
}

func build(t *testing.T, user *entity.User, repo *fakeRepo, cache *fakeCache) usecase.AuthUseCaseItf {
	t.Helper()

	repo.user = user

	return usecase.NewAuthUseCase(repo, fakeJWT{}, cache, &env.Env{JWTRefreshExpiredDays: 30, JWTSessionMaxDays: 90, JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests", DeviceTokenTTLDays: 90})
}

func accountWith(t *testing.T, password string) *entity.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	return &entity.User{ID: uuid.New(), Email: "a@example.com", Role: constants.RoleWali, PasswordHash: string(hash)}
}

func TestLoginDoesNotAcceptACharacterBeyondBcryptLimit(t *testing.T) {
	// 72 bytes but 71 characters, so one more character still passes the length validation
	registered := strings.Repeat("a", 70) + "é"
	useCase := build(t, accountWith(t, registered), &fakeRepo{}, newFakeCache())

	_, err := useCase.Login(context.Background(), dto.LoginRequest{Email: "a@example.com", Password: registered + "z", Role: constants.RoleWali})
	require.Equal(t, "AUTH_INVALID_CREDENTIALS", apperror.As(err).Code)

	res, err := useCase.Login(context.Background(), dto.LoginRequest{Email: "a@example.com", Password: registered, Role: constants.RoleWali})
	require.NoError(t, err)
	require.Equal(t, "access", res.Token)
}

func TestRefreshCanBeRetriedAfterADatabaseFailure(t *testing.T) {
	cache := newFakeCache()
	repo := &fakeRepo{rotateErrs: []error{errors.New("connection reset"), nil}}
	useCase := build(t, accountWith(t, "password-1234"), repo, cache)

	// Redis remembers a token that was issued earlier
	cache.keys[constants.RefreshKeyPrefix+hash("old-token")] = "user"

	_, err := useCase.Refresh(context.Background(), "old-token")
	require.Equal(t, http.StatusInternalServerError, apperror.As(err).Status)

	// the database never consumed the token, so the client may try again at once
	res, err := useCase.Refresh(context.Background(), "old-token")
	require.NoError(t, err)
	require.NotEmpty(t, res.RefreshToken)
}

func TestRefreshStillRejectsAReplay(t *testing.T) {
	cache := newFakeCache()
	repo := &fakeRepo{}
	useCase := build(t, accountWith(t, "password-1234"), repo, cache)

	cache.keys[constants.RefreshKeyPrefix+hash("old-token")] = "user"

	_, err := useCase.Refresh(context.Background(), "old-token")
	require.NoError(t, err)

	_, err = useCase.Refresh(context.Background(), "old-token")
	require.Equal(t, "AUTH_REFRESH_INVALID", apperror.As(err).Code)
	require.Equal(t, 1, repo.rotateCalls)
}

// hash mirrors how the use case names a token in the cache
func hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(sum[:])
}

func deviceTokens() *devicetoken.Tokens {
	return devicetoken.New(&env.Env{JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests", DeviceTokenTTLDays: 90})
}

func TestLoginHandsTheDeviceATokenForItsEmail(t *testing.T) {
	useCase := build(t, accountWith(t, "correct horse"), &fakeRepo{}, newFakeCache())

	res, err := useCase.Login(context.Background(), dto.LoginRequest{Email: "a@example.com", Password: "correct horse", Role: constants.RoleWali})
	require.NoError(t, err)
	require.NotEmpty(t, res.DeviceToken)

	_, ok := deviceTokens().Verify(res.DeviceToken, "a@example.com")
	require.True(t, ok)

	_, ok = deviceTokens().Verify(res.DeviceToken, "someone.else@example.com")
	require.False(t, ok)
}

func TestLoginKeepsTheDeviceIdOfAValidTokenAndIgnoresOthers(t *testing.T) {
	useCase := build(t, accountWith(t, "correct horse"), &fakeRepo{}, newFakeCache())
	login := func(presented string) string {
		res, err := useCase.Login(context.Background(), dto.LoginRequest{
			Email: "a@example.com", Password: "correct horse", Role: constants.RoleWali, DeviceToken: presented,
		})
		require.NoError(t, err)

		return res.DeviceToken
	}

	first := login("")
	id := func(token string) []byte {
		device, ok := deviceTokens().Verify(token, "a@example.com")
		require.True(t, ok)

		return device
	}

	require.Equal(t, id(first), id(login(first)), "signing in again on the same device keeps its id")
	require.NotEqual(t, id(first), id(login("not-a-token")), "an invalid token is ignored")

	foreign, err := deviceTokens().Issue("other@example.com", "")
	require.NoError(t, err)
	require.NotEqual(t, id(first), id(login(foreign)), "the token of another email is ignored")
}

func TestAFailedLoginHandsOutNoToken(t *testing.T) {
	useCase := build(t, accountWith(t, "correct horse"), &fakeRepo{}, newFakeCache())

	res, err := useCase.Login(context.Background(), dto.LoginRequest{Email: "a@example.com", Password: "wrong", Role: constants.RoleWali})
	require.Error(t, err)
	require.Empty(t, res.DeviceToken)
}

// The account and its first refresh token are stored together, so no failure in between can leave an account without a session
func TestRegistrationStoresTheFirstSessionWithTheAccount(t *testing.T) {
	register := func(t *testing.T, repo *fakeRepo, role constants.Role) (dto.AuthResponse, error) {
		t.Helper()

		useCase := build(t, nil, repo, newFakeCache())
		req := dto.RegisterRequest{
			Email: "new@example.com", Password: "correct horse", FullName: "New Person", Role: role,
			Class: &dto.RegisterClass{Name: "Bunga"}, ClassCode: "ABC234", Student: &dto.RegisterStudent{NISN: "12345", FullName: "Anak"},
		}

		return useCase.Register(context.Background(), req)
	}

	for _, role := range []constants.Role{constants.RoleGuru, constants.RoleWali} {
		t.Run(string(role), func(t *testing.T) {
			repo := &fakeRepo{}

			res, err := register(t, repo, role)
			require.NoError(t, err)

			require.NotNil(t, repo.registered, "the session has to reach the repository with the account")
			require.Equal(t, 0, repo.separateTokens, "no second write that could fail on its own")
			require.NotEqual(t, uuid.Nil, repo.registered.UserID)
			require.NotEmpty(t, res.RefreshToken)
			require.NotEmpty(t, res.DeviceToken)
			require.Equal(t, sha256Hex(res.RefreshToken), repo.registered.TokenHash, "the stored token is the hash of the one returned")
		})
	}

	t.Run("a failed registration returns no token", func(t *testing.T) {
		repo := &fakeRepo{createErr: apperror.ErrEmailTaken}

		res, err := register(t, repo, constants.RoleGuru)

		require.ErrorIs(t, err, apperror.ErrEmailTaken)
		require.Empty(t, res.RefreshToken)
		require.Nil(t, repo.registered)
	})
}

func TestRegistrationFailsWhenTheAccessTokenCannotBeSigned(t *testing.T) {
	repo := &fakeRepo{}
	useCase := usecase.NewAuthUseCase(repo, brokenJWT{}, newFakeCache(), &env.Env{JWTRefreshExpiredDays: 30, JWTSessionMaxDays: 90, JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests", DeviceTokenTTLDays: 90})

	res, err := useCase.Register(context.Background(), dto.RegisterRequest{
		Email: "new@example.com", Password: "correct horse", FullName: "New Person", Role: constants.RoleGuru, Class: &dto.RegisterClass{Name: "Bunga"},
	})

	var appErr *apperror.Error

	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusInternalServerError, appErr.Status)
	require.Empty(t, res.RefreshToken)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

func TestRefreshTellsTheRepositoryTheLimitsOfASession(t *testing.T) {
	repo := &fakeRepo{}
	useCase := build(t, accountWith(t, "password-1234"), repo, newFakeCache())

	_, err := useCase.Refresh(context.Background(), "old-token")
	require.NoError(t, err)

	require.Equal(t, 90*24*time.Hour, repo.policy.SessionMax)
	require.Equal(t, time.Minute, repo.policy.ReuseGrace, "the same minute that Redis flags a used token for")
}

func TestRefreshRefusesATokenThatCannotBeUsed(t *testing.T) {
	for name, rotation := range map[string]repository.Rotation{
		"unknown, expired or used a moment ago": {},
		"used before and the session ended":     {Reused: true, UserID: uuid.New()},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{rotation: &rotation}
			useCase := build(t, accountWith(t, "password-1234"), repo, newFakeCache())

			res, err := useCase.Refresh(context.Background(), "old-token")

			require.Equal(t, "AUTH_REFRESH_INVALID", apperror.As(err).Code)
			require.Empty(t, res.RefreshToken)
		})
	}
}

func TestAReusedTokenIsLoggedWithTheOwner(t *testing.T) {
	var output bytes.Buffer

	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	owner := uuid.New()
	repo := &fakeRepo{rotation: &repository.Rotation{Reused: true, UserID: owner}}
	useCase := build(t, accountWith(t, "password-1234"), repo, newFakeCache())

	_, err := useCase.Refresh(context.Background(), "old-token")

	require.Equal(t, "AUTH_REFRESH_INVALID", apperror.As(err).Code)
	require.Contains(t, output.String(), owner.String())
	require.NotContains(t, output.String(), "old-token", "the token itself is never logged")
}

func TestASessionStartsWithItsOwnFamilyAndNeverOutlastsItsMaximum(t *testing.T) {
	repo := &fakeRepo{}
	useCase := usecase.NewAuthUseCase(repo, fakeJWT{}, newFakeCache(), &env.Env{
		JWTRefreshExpiredDays: 30, JWTSessionMaxDays: 7, DeviceTokenTTLDays: 90,
		JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests",
	})

	_, err := useCase.Register(context.Background(), dto.RegisterRequest{
		Email: "new@example.com", Password: "correct horse", FullName: "New Person", Role: constants.RoleGuru,
		Class: &dto.RegisterClass{Name: "Bunga"},
	})
	require.NoError(t, err)

	session := repo.registered
	require.NotEqual(t, uuid.Nil, session.FamilyID)
	require.Equal(t, session.CreatedAt, session.SessionStartedAt)
	require.Equal(t, 7*24*time.Hour, session.ExpiresAt.Sub(session.CreatedAt), "the shorter of the two limits")
}
