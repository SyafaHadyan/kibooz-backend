package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/trash/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

// historyRepo answers the access checks and the saved standings from memory, and remembers what it was asked
type historyRepo struct {
	repository.TrashDBItf

	guruID   *uuid.UUID
	teaches  bool
	labels   []string
	total    int
	rows     []repository.HistoryRow
	seasonsE error
	entriesE error

	askedClass  uuid.UUID
	askedLimit  int
	askedOffset int
	askedLabels []string
}

func (r *historyRepo) FindGuruIDByUserID(context.Context, uuid.UUID) (*uuid.UUID, error) {
	return r.guruID, nil
}

func (r *historyRepo) FirstGuruClass(context.Context, uuid.UUID) (*uuid.UUID, error) {
	id := uuid.New()

	return &id, nil
}

func (r *historyRepo) GuruTeachesClass(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return r.teaches, nil
}

func (r *historyRepo) ListHistorySeasons(_ context.Context, classID uuid.UUID, limit int, offset int) ([]string, int, error) {
	r.askedClass, r.askedLimit, r.askedOffset = classID, limit, offset

	return r.labels, r.total, r.seasonsE
}

func (r *historyRepo) ListHistoryEntries(_ context.Context, _ uuid.UUID, labels []string) ([]repository.HistoryRow, error) {
	r.askedLabels = labels

	return r.rows, r.entriesE
}

func historyCase(repo *historyRepo) TrashUseCaseItf {
	return NewTrashUseCase(repo, nil, nil, &env.Env{})
}

func teacher() *uuid.UUID {
	id := uuid.New()

	return &id
}

func place(label string, rank int, name string, points int) repository.HistoryRow {
	return repository.HistoryRow{SeasonLabel: label, RankPosition: rank, StudentName: name, Points: points}
}

func TestLeaderboardHistoryGroupsEachSemesterIntoPodiumAndRankings(t *testing.T) {
	repo := &historyRepo{
		guruID: teacher(), teaches: true, total: 3,
		labels: []string{"2026-07", "2026-01"},
		rows: []repository.HistoryRow{
			place("2026-07", 1, "Zahra", 90), place("2026-07", 2, "Amelia", 80), place("2026-07", 3, "Kenzo", 70), place("2026-07", 4, "Rizky", 60),
			place("2026-01", 1, "Amelia", 40),
		},
	}
	classID := uuid.New()

	res, err := historyCase(repo).LeaderboardHistory(
		context.Background(), uuid.New(), constants.RoleGuru, &classID, pagination.Params{Page: 2, Limit: 2},
	)

	require.NoError(t, err)
	require.Equal(t, dto.PageInfo{Page: 2, Limit: 2, Total: 3}, res.PageInfo)
	require.Len(t, res.Seasons, 2)
	require.Equal(t, "2026-07", res.Seasons[0].Label, "newest semester first")
	require.Len(t, res.Seasons[0].Podium, 3)
	require.Equal(t, dto.LeaderboardHistoryEntry{Rank: 4, StudentName: "Rizky", Points: 60}, res.Seasons[0].Rankings[0])
	require.Equal(t, "2026-01", res.Seasons[1].Label)
	require.Len(t, res.Seasons[1].Podium, 1)
	require.Empty(t, res.Seasons[1].Rankings)
	require.NotNil(t, res.Seasons[1].Rankings, "an empty list is not null")

	require.Equal(t, classID, repo.askedClass)
	require.Equal(t, 2, repo.askedLimit)
	require.Equal(t, 2, repo.askedOffset, "the second page of two starts after two semesters")
	require.Equal(t, []string{"2026-07", "2026-01"}, repo.askedLabels)
}

func TestLeaderboardHistoryWithNoSemesterIsEmptyAndNotNull(t *testing.T) {
	repo := &historyRepo{guruID: teacher(), teaches: true}

	res, err := historyCase(repo).LeaderboardHistory(
		context.Background(), uuid.New(), constants.RoleGuru, nil, pagination.Params{Page: 1, Limit: 20},
	)

	require.NoError(t, err)
	require.NotNil(t, res.Seasons)
	require.Empty(t, res.Seasons)
	require.Zero(t, res.Total)
}

func TestBuildHistoryKeepsAShorterPodiumWhenAChildWasDeleted(t *testing.T) {
	res := buildHistory(
		[]string{"2026-01"},
		[]repository.HistoryRow{place("2026-01", 2, "Amelia", 80), place("2026-01", 3, "Kenzo", 70), place("2026-01", 5, "Rizky", 60)},
		dto.PageInfo{Page: 1, Limit: 20, Total: 1},
	)

	require.Len(t, res.Seasons[0].Podium, 2, "the place of the deleted first child is not filled")
	require.Equal(t, 2, res.Seasons[0].Podium[0].Rank)
	require.Len(t, res.Seasons[0].Rankings, 1)
	require.Equal(t, 5, res.Seasons[0].Rankings[0].Rank)
}

func TestBuildHistoryIgnoresARowOfAnotherSemester(t *testing.T) {
	res := buildHistory([]string{"2026-01"}, []repository.HistoryRow{place("2025-07", 1, "Old", 10)}, dto.PageInfo{})

	require.Empty(t, res.Seasons[0].Podium)
	require.Empty(t, res.Seasons[0].Rankings)
}

func TestLeaderboardHistoryChecksAccessBeforeReadingAnything(t *testing.T) {
	page := pagination.Params{Page: 1, Limit: 20}
	other := uuid.New()

	t.Run("a teacher of another class", func(t *testing.T) {
		repo := &historyRepo{guruID: teacher(), teaches: false}

		_, err := historyCase(repo).LeaderboardHistory(context.Background(), uuid.New(), constants.RoleGuru, &other, page)

		require.ErrorIs(t, err, apperror.ErrForbidden)
		require.Nil(t, repo.askedLabels)
	})

	t.Run("a teacher without a profile", func(t *testing.T) {
		repo := &historyRepo{}

		_, err := historyCase(repo).LeaderboardHistory(context.Background(), uuid.New(), constants.RoleGuru, nil, page)

		require.ErrorIs(t, err, apperror.ErrProfileNotFound)
	})

	t.Run("an admin", func(t *testing.T) {
		_, err := historyCase(&historyRepo{}).LeaderboardHistory(context.Background(), uuid.New(), constants.RoleAdmin, nil, page)

		require.ErrorIs(t, err, apperror.ErrForbidden)
	})
}

func TestLeaderboardHistoryTurnsStorageFailuresIntoInternalErrors(t *testing.T) {
	page := pagination.Params{Page: 1, Limit: 20}
	boom := errors.New("database is down")

	for name, repo := range map[string]*historyRepo{
		"listing the semesters": {guruID: teacher(), teaches: true, seasonsE: boom},
		"listing the places":    {guruID: teacher(), teaches: true, labels: []string{"2026-01"}, entriesE: boom},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := historyCase(repo).LeaderboardHistory(context.Background(), uuid.New(), constants.RoleGuru, nil, page)

			var failure *apperror.Error

			require.ErrorAs(t, err, &failure)
			require.Equal(t, 500, failure.Status)
		})
	}
}
