package usecase

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

func TestPointsFor(t *testing.T) {
	cfg := &env.Env{PointsOrganik: 10, PointsAnorganik: 15, PointsB3: 0}

	require.Equal(t, 10, PointsFor(cfg, constants.TrashOrganik))
	require.Equal(t, 15, PointsFor(cfg, constants.TrashAnorganik))
	require.Equal(t, 0, PointsFor(cfg, constants.TrashB3))
}

func student(name string, points int) entity.Student {
	return entity.Student{ID: uuid.New(), FullName: name, CurrentPoints: points}
}

func TestBuildLeaderboardSplitsPodiumAndRankings(t *testing.T) {
	res := buildLeaderboard([]entity.Student{
		student("Farhan", 420), student("Amelia", 355), student("Kenzo", 310),
		student("Zhafira", 290), student("Rizky", 275),
	})

	require.Len(t, res.Podium, 3)
	require.Len(t, res.Rankings, 2)
	require.Equal(t, 1, res.Podium[0].Rank)
	require.Equal(t, "Farhan", res.Podium[0].StudentName)
	require.Equal(t, 4, res.Rankings[0].Rank)
	require.Equal(t, "Rizky", res.Rankings[1].StudentName)
}

func TestBuildLeaderboardNeverReturnsNilSlices(t *testing.T) {
	res := buildLeaderboard(nil)

	require.NotNil(t, res.Podium)
	require.NotNil(t, res.Rankings)
	require.Empty(t, res.Podium)
	require.Empty(t, res.Rankings)

	res = buildLeaderboard([]entity.Student{student("Solo", 10)})
	require.Len(t, res.Podium, 1)
	require.Empty(t, res.Rankings)
}
