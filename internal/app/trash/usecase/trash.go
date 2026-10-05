// Package usecase holds the gamification business rules
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/trash/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/clock"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

const podiumSize = 3

type TrashUseCaseItf interface {
	ScanClaim(ctx context.Context, userID uuid.UUID, req dto.ScanClaimRequest) (dto.ScanClaimResponse, error)
	Leaderboard(ctx context.Context, userID uuid.UUID, role constants.Role, classID *uuid.UUID) (dto.LeaderboardResponse, error)
}

type TrashUseCase struct {
	repo    repository.TrashDBItf
	cache   redis.CacheItf
	storage s3.StorageItf
	cfg     *env.Env
	now     func() time.Time
}

func NewTrashUseCase(repo repository.TrashDBItf, cache redis.CacheItf, storage s3.StorageItf, cfg *env.Env) TrashUseCaseItf {
	return &TrashUseCase{repo: repo, cache: cache, storage: storage, cfg: cfg, now: time.Now}
}

// PointsFor returns the reward of one verified action
func PointsFor(cfg *env.Env, trashType constants.TrashType) int {
	switch trashType {
	case constants.TrashOrganik:
		return cfg.PointsOrganik
	case constants.TrashAnorganik:
		return cfg.PointsAnorganik
	default:
		return cfg.PointsB3
	}
}

func (u *TrashUseCase) ScanClaim(ctx context.Context, userID uuid.UUID, req dto.ScanClaimRequest) (dto.ScanClaimResponse, error) {
	waliID, err := u.repo.FindWaliIDByUserID(ctx, userID)
	if err != nil {
		return dto.ScanClaimResponse{}, apperror.Internal(err)
	}

	if waliID == nil {
		return dto.ScanClaimResponse{}, apperror.ErrProfileNotFound
	}

	student, err := u.repo.FindOwnedStudent(ctx, *waliID, req.StudentID)
	if err != nil {
		return dto.ScanClaimResponse{}, apperror.Internal(err)
	}

	if student == nil {
		return dto.ScanClaimResponse{}, apperror.ErrStudentNotFound
	}

	var photo *imageutil.Image

	if req.PhotoBase64 != "" {
		decoded, err := imageutil.DecodeBase64(req.PhotoBase64, constants.TrashPhotoMaxBytes)
		if err != nil {
			return dto.ScanClaimResponse{}, err
		}

		photo = &decoded
	}

	now := u.now()
	from, to := clock.DayBounds(now, u.cfg.Location())

	// cheap early check so a rejected claim never uploads a photo
	used, err := u.repo.CountScans(ctx, student.ID, from, to)
	if err != nil {
		return dto.ScanClaimResponse{}, apperror.Internal(err)
	}

	if used >= u.cfg.TrashDailyLimit {
		return dto.ScanClaimResponse{}, apperror.ErrDailyLimitReached
	}

	scan := &entity.TrashScan{
		ID:              uuid.New(),
		StudentID:       student.ID,
		TrashType:       req.TrashType,
		ConfidenceScore: req.ConfidenceScore,
		PointsAwarded:   PointsFor(u.cfg, req.TrashType),
		ScannedAt:       now.UTC(),
	}

	if photo != nil && u.storage.Enabled() {
		key := fmt.Sprintf("%s/%s/%s%s", constants.TrashDirectory, student.ID, scan.ID, photo.Extension)

		url, err := u.storage.Upload(ctx, key, photo.ContentType, photo.Data)
		if err != nil {
			return dto.ScanClaimResponse{}, err
		}

		scan.PhotoURL = &url
	}

	result, err := u.repo.Claim(ctx, scan, student.ClassID, u.cfg.TrashDailyLimit, from, to)
	if err != nil {
		return dto.ScanClaimResponse{}, apperror.As(err)
	}

	// the cache is optional, entries also expire by themselves
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+student.ClassID.String())

	return dto.ScanClaimResponse{
		PointsAdded:         scan.PointsAwarded,
		TotalPoints:         result.TotalPoints,
		NewRank:             result.Rank,
		RemainingDailyScans: result.Remaining,
	}, nil
}

func (u *TrashUseCase) Leaderboard(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID *uuid.UUID,
) (dto.LeaderboardResponse, error) {
	resolved, err := u.authorizeClass(ctx, userID, role, classID)
	if err != nil {
		return dto.LeaderboardResponse{}, err
	}

	cacheKey := constants.LeaderboardKeyPrefix + resolved.String()

	cached, found, _ := u.cache.Get(ctx, cacheKey)
	if found {
		var res dto.LeaderboardResponse

		if json.Unmarshal([]byte(cached), &res) == nil {
			return res, nil
		}
	}

	students, err := u.repo.ListClassStudents(ctx, resolved)
	if err != nil {
		return dto.LeaderboardResponse{}, apperror.Internal(err)
	}

	res := buildLeaderboard(students)

	encoded, err := json.Marshal(res)
	if err == nil {
		ttl := time.Duration(u.cfg.LeaderboardCacheSeconds) * time.Second

		_ = u.cache.Set(ctx, cacheKey, string(encoded), ttl)
	}

	return res, nil
}

// authorizeClass resolves the class to show and confirms the caller belongs to it
func (u *TrashUseCase) authorizeClass(
	ctx context.Context, userID uuid.UUID, role constants.Role, classID *uuid.UUID,
) (uuid.UUID, error) {
	switch role {
	case constants.RoleWali:
		waliID, err := u.repo.FindWaliIDByUserID(ctx, userID)
		if err != nil {
			return uuid.Nil, apperror.Internal(err)
		}

		if waliID == nil {
			return uuid.Nil, apperror.ErrProfileNotFound
		}

		if classID == nil {
			first, err := u.repo.FirstWaliClass(ctx, *waliID)
			if err != nil {
				return uuid.Nil, apperror.Internal(err)
			}

			if first == nil {
				return uuid.Nil, apperror.ErrClassNotFound
			}

			return *first, nil
		}

		member, err := u.repo.WaliHasStudentInClass(ctx, *waliID, *classID)
		if err != nil {
			return uuid.Nil, apperror.Internal(err)
		}

		if !member {
			return uuid.Nil, apperror.ErrForbidden
		}

		return *classID, nil
	case constants.RoleGuru:
		guruID, err := u.repo.FindGuruIDByUserID(ctx, userID)
		if err != nil {
			return uuid.Nil, apperror.Internal(err)
		}

		if guruID == nil {
			return uuid.Nil, apperror.ErrProfileNotFound
		}

		if classID == nil {
			first, err := u.repo.FirstGuruClass(ctx, *guruID)
			if err != nil {
				return uuid.Nil, apperror.Internal(err)
			}

			if first == nil {
				return uuid.Nil, apperror.ErrClassNotFound
			}

			return *first, nil
		}

		teaches, err := u.repo.GuruTeachesClass(ctx, *guruID, *classID)
		if err != nil {
			return uuid.Nil, apperror.Internal(err)
		}

		if !teaches {
			return uuid.Nil, apperror.ErrForbidden
		}

		return *classID, nil
	default:
		return uuid.Nil, apperror.ErrForbidden
	}
}

// buildLeaderboard expects students already sorted by points, then name, then id
func buildLeaderboard(students []entity.Student) dto.LeaderboardResponse {
	res := dto.LeaderboardResponse{
		Podium:   make([]dto.LeaderboardEntry, 0, podiumSize),
		Rankings: make([]dto.LeaderboardEntry, 0, max(len(students)-podiumSize, 0)),
	}

	for i, student := range students {
		entry := dto.LeaderboardEntry{
			Rank:        i + 1,
			StudentName: student.FullName,
			Points:      student.CurrentPoints,
			AvatarURL:   student.AvatarURL,
		}

		if i < podiumSize {
			res.Podium = append(res.Podium, entry)
		} else {
			res.Rankings = append(res.Rankings, entry)
		}
	}

	return res
}
