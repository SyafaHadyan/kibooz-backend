package usecase_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

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
}

func (f *fakeRepo) FindUserByEmail(context.Context, string) (*entity.User, error) { return f.user, nil }

func (f *fakeRepo) FindUserByID(context.Context, uuid.UUID) (*entity.User, error) { return f.user, nil }

func (f *fakeRepo) CreateGuru(context.Context, *entity.User, *entity.Guru, *entity.Class) error {
	return nil
}

func (f *fakeRepo) CreateWali(context.Context, *entity.User, *entity.Wali, string, *entity.Student) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (f *fakeRepo) CreateRefreshToken(context.Context, *entity.RefreshToken) error { return nil }

func (f *fakeRepo) RotateRefreshToken(context.Context, string, time.Time, *entity.RefreshToken) (*entity.User, error) {
	f.rotateCalls++

	if len(f.rotateErrs) > 0 {
		err := f.rotateErrs[0]
		f.rotateErrs = f.rotateErrs[1:]

		if err != nil {
			return nil, err
		}
	}

	return f.user, nil
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

func build(t *testing.T, user *entity.User, repo *fakeRepo, cache *fakeCache) usecase.AuthUseCaseItf {
	t.Helper()

	repo.user = user

	return usecase.NewAuthUseCase(repo, fakeJWT{}, cache, &env.Env{JWTRefreshExpiredDays: 30, JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests", DeviceTokenTTLDays: 90})
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
