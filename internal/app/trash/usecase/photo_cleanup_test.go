package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/trash/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
)

// a 1x1 PNG as a data URI
const tinyPNGDataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

type claimRepo struct {
	repository.TrashDBItf

	student  *entity.Student
	claimErr error
}

func (r *claimRepo) FindWaliIDByUserID(context.Context, uuid.UUID) (*uuid.UUID, error) {
	id := uuid.New()

	return &id, nil
}

func (r *claimRepo) FindOwnedStudent(context.Context, uuid.UUID, uuid.UUID) (*entity.Student, error) {
	return r.student, nil
}

func (r *claimRepo) CountScans(context.Context, uuid.UUID, time.Time, time.Time) (int, error) {
	return 0, nil
}

func (r *claimRepo) Claim(context.Context, *entity.TrashScan, uuid.UUID, int, time.Time, time.Time) (repository.ClaimResult, error) {
	return repository.ClaimResult{TotalPoints: 10, Rank: 1, Remaining: 4}, r.claimErr
}

type quietCache struct{ redis.CacheItf }

func (quietCache) Del(context.Context, ...string) error { return nil }

type recordingStorage struct {
	uploaded  []string
	deleted   []string
	deleteErr error
}

func (s *recordingStorage) Enabled() bool { return true }

func (s *recordingStorage) KeyFromURL(string) (string, bool) { return "", false }

func (s *recordingStorage) Upload(_ context.Context, key string, _ string, _ []byte) (string, error) {
	s.uploaded = append(s.uploaded, key)

	return "https://cdn.example.com/" + key, nil
}

func (s *recordingStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)

	return s.deleteErr
}

func claimWith(t *testing.T, repo *claimRepo, storage *recordingStorage, photo string) (dto.ScanClaimResponse, error) {
	t.Helper()

	cfg := &env.Env{AppTimezone: "Asia/Jakarta", TrashDailyLimit: 5, PointsOrganik: 10}
	useCase := NewTrashUseCase(repo, quietCache{}, storage, cfg)

	return useCase.ScanClaim(context.Background(), uuid.New(), dto.ScanClaimRequest{
		StudentID: repo.student.ID, TrashType: constants.TrashOrganik, ConfidenceScore: 0.9, PhotoBase64: photo,
	})
}

func newClaimRepo(claimErr error) *claimRepo {
	return &claimRepo{student: &entity.Student{ID: uuid.New(), ClassID: uuid.New()}, claimErr: claimErr}
}

func TestFailedClaimRemovesItsUploadedPhoto(t *testing.T) {
	storage := &recordingStorage{}

	_, err := claimWith(t, newClaimRepo(apperror.ErrDailyLimitReached), storage, tinyPNGDataURI)

	require.Equal(t, "TRASH_DAILY_LIMIT_REACHED", apperror.As(err).Code)
	require.Len(t, storage.uploaded, 1)
	require.Equal(t, storage.uploaded, storage.deleted, "the photo that was stored is the one removed")
}

func TestSuccessfulClaimKeepsItsPhoto(t *testing.T) {
	storage := &recordingStorage{}

	res, err := claimWith(t, newClaimRepo(nil), storage, tinyPNGDataURI)

	require.NoError(t, err)
	require.Equal(t, 10, res.PointsAdded)
	require.Len(t, storage.uploaded, 1)
	require.Empty(t, storage.deleted)
}

func TestClaimWithoutAPhotoTouchesNoStorage(t *testing.T) {
	storage := &recordingStorage{}

	_, err := claimWith(t, newClaimRepo(apperror.ErrDailyLimitReached), storage, "")

	require.Error(t, err)
	require.Empty(t, storage.uploaded)
	require.Empty(t, storage.deleted)
}

func TestFailedCleanupDoesNotReplaceTheClaimError(t *testing.T) {
	storage := &recordingStorage{deleteErr: errors.New("bucket unreachable")}

	_, err := claimWith(t, newClaimRepo(apperror.ErrDailyLimitReached), storage, tinyPNGDataURI)

	require.Equal(t, "TRASH_DAILY_LIMIT_REACHED", apperror.As(err).Code)
	require.Len(t, storage.deleted, 1)
}
