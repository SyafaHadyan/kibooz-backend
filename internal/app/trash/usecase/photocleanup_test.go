package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/app/trash/repository"
	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/domain/entity"
	"github.com/kiboozapp/kibooz-backend/internal/infra/env"
	"github.com/kiboozapp/kibooz-backend/internal/infra/redis"
	"github.com/kiboozapp/kibooz-backend/internal/infra/s3"
)

// a 1x1 PNG as a data URI
const tinyPNGDataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

type claimRepo struct {
	repository.TrashDBItf

	student  *entity.Student
	claimErr error
	reduced  bool

	gotLimit     int
	gotOverLimit int
	gotPoints    int
}

func (r *claimRepo) FindWaliIDByUserID(context.Context, uuid.UUID) (*uuid.UUID, error) {
	id := uuid.New()

	return &id, nil
}

func (r *claimRepo) FindOwnedStudent(context.Context, uuid.UUID, uuid.UUID) (*entity.Student, error) {
	return r.student, nil
}

func (r *claimRepo) Claim(
	_ context.Context, scan *entity.TrashScan, _ uuid.UUID, limit int, overLimitPoints int, _ time.Time, _ time.Time,
) (repository.ClaimResult, error) {
	r.gotLimit, r.gotOverLimit, r.gotPoints = limit, overLimitPoints, scan.PointsAwarded

	if r.reduced {
		scan.PointsAwarded = min(scan.PointsAwarded, overLimitPoints)
	}

	return repository.ClaimResult{TotalPoints: 10, Rank: 1, Remaining: 4, Reduced: r.reduced}, r.claimErr
}

type quietCache struct{ redis.CacheItf }

func (quietCache) Del(context.Context, ...string) error { return nil }

type recordingStorage struct {
	s3.Disabled

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

	cfg := &env.Env{AppTimezone: "Asia/Jakarta", TrashDailyLimit: 5, TrashOverLimitPoints: 1, PointsOrganik: 10}
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

	_, err := claimWith(t, newClaimRepo(apperror.ErrStudentNotFound), storage, tinyPNGDataURI)

	require.Equal(t, "STUDENT_NOT_FOUND", apperror.As(err).Code)
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

	_, err := claimWith(t, newClaimRepo(apperror.ErrStudentNotFound), storage, "")

	require.Error(t, err)
	require.Empty(t, storage.uploaded)
	require.Empty(t, storage.deleted)
}

func TestFailedCleanupDoesNotReplaceTheClaimError(t *testing.T) {
	storage := &recordingStorage{deleteErr: errors.New("bucket unreachable")}

	_, err := claimWith(t, newClaimRepo(apperror.ErrStudentNotFound), storage, tinyPNGDataURI)

	require.Equal(t, "STUDENT_NOT_FOUND", apperror.As(err).Code)
	require.Len(t, storage.deleted, 1)
}

func TestClaimPassesTheFullPointsAndTheOverLimitRuleToTheRepository(t *testing.T) {
	repo := newClaimRepo(nil)

	res, err := claimWith(t, repo, &recordingStorage{}, "")

	require.NoError(t, err)
	require.Equal(t, 5, repo.gotLimit)
	require.Equal(t, 1, repo.gotOverLimit)
	require.Equal(t, 10, repo.gotPoints, "the use case asks for the full points and the repository lowers them")
	require.False(t, res.ReducedPoints)
	require.Equal(t, 10, res.PointsAdded)
}

func TestClaimPastTheDailyLimitReportsTheReducedPoints(t *testing.T) {
	repo := newClaimRepo(nil)
	repo.reduced = true

	res, err := claimWith(t, repo, &recordingStorage{}, "")

	require.NoError(t, err)
	require.True(t, res.ReducedPoints)
	require.Equal(t, 1, res.PointsAdded)
}
