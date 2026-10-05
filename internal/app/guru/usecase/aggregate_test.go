package usecase

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

func record(student uuid.UUID, mood constants.Mood, at time.Time) repository.MoodRecord {
	return repository.MoodRecord{StudentID: student, MoodType: mood, RecordedAt: at}
}

func jakarta(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	return loc
}

func TestLatestPerStudentPerDayKeepsNewestLog(t *testing.T) {
	loc := jakarta(t)
	student := uuid.New()
	morning := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)

	items := latestPerStudentPerDay([]repository.MoodRecord{
		record(student, constants.MoodSedih, morning),
		record(student, constants.MoodSenang, morning.Add(2*time.Hour)),
	}, loc)

	require.Len(t, items, 1)
	require.Equal(t, constants.MoodSenang, items[0].Mood)
	require.Equal(t, "2026-10-05", items[0].Day)
}

func TestLatestPerStudentPerDaySplitsDaysInSchoolTimezone(t *testing.T) {
	loc := jakarta(t)
	student := uuid.New()

	// 16:59 UTC and 17:01 UTC fall on different Jakarta days
	items := latestPerStudentPerDay([]repository.MoodRecord{
		record(student, constants.MoodSedih, time.Date(2026, 10, 5, 16, 59, 0, 0, time.UTC)),
		record(student, constants.MoodSenang, time.Date(2026, 10, 5, 17, 1, 0, 0, time.UTC)),
	}, loc)

	require.Len(t, items, 2)
	require.Equal(t, "2026-10-05", items[0].Day)
	require.Equal(t, "2026-10-06", items[1].Day)
}

func TestDonutSlicesAlwaysSumToOneHundred(t *testing.T) {
	cases := []map[constants.Mood]int{
		{constants.MoodSenang: 1, constants.MoodSedih: 1, constants.MoodMarah: 1},
		{constants.MoodSenang: 14, constants.MoodSedih: 3, constants.MoodMarah: 1, constants.MoodBingung: 4},
		{constants.MoodSenang: 7},
		{constants.MoodSenang: 1, constants.MoodBingung: 2, constants.MoodSedih: 2, constants.MoodMarah: 2},
	}

	for _, counts := range cases {
		total := 0
		for _, slice := range donutSlices(counts) {
			total += slice.Percentage
		}

		require.Equal(t, 100, total, "counts %v", counts)
	}
}

func TestDonutSlicesSortedAndColored(t *testing.T) {
	slices := donutSlices(map[constants.Mood]int{
		constants.MoodSenang: 58, constants.MoodBingung: 20, constants.MoodSedih: 14, constants.MoodMarah: 8,
	})

	require.Len(t, slices, 4)
	require.Equal(t, constants.MoodSenang, slices[0].Mood)
	require.Equal(t, 58, slices[0].Percentage)
	require.Equal(t, "#34C759", slices[0].ColorHex)
	require.Equal(t, constants.MoodMarah, slices[3].Mood)
	require.Equal(t, "#FF3B30", slices[3].ColorHex)
}

func TestDonutSlicesWithoutDataIsAllZero(t *testing.T) {
	slices := donutSlices(map[constants.Mood]int{})

	require.Len(t, slices, 4)

	for _, slice := range slices {
		require.Zero(t, slice.Percentage)
	}
}

func TestDominantMood(t *testing.T) {
	require.Nil(t, dominantMood(map[constants.Mood]int{}))

	got := dominantMood(map[constants.Mood]int{constants.MoodSedih: 3, constants.MoodMarah: 1})
	require.NotNil(t, got)
	require.Equal(t, constants.MoodSedih, *got)

	// ties prefer the chart order so SENANG wins
	got = dominantMood(map[constants.Mood]int{constants.MoodSenang: 2, constants.MoodMarah: 2})
	require.Equal(t, constants.MoodSenang, *got)
}

func TestWeeklyTrendScoresShareOfHappyStudents(t *testing.T) {
	loc := jakarta(t)
	weekStart := time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC) // Monday 28 Sep in Jakarta

	items := []dayMood{
		{Day: "2026-09-28", Student: uuid.New(), Mood: constants.MoodSenang},
		{Day: "2026-09-28", Student: uuid.New(), Mood: constants.MoodSedih},
		{Day: "2026-09-30", Student: uuid.New(), Mood: constants.MoodSenang},
		// weekend entries never appear in the Monday to Friday chart
		{Day: "2026-10-03", Student: uuid.New(), Mood: constants.MoodSenang},
	}

	points := weeklyTrend(items, weekStart, loc)

	require.Len(t, points, 5)
	require.Equal(t, "Senin", points[0].Day)
	require.Equal(t, 50, points[0].AverageHappyScore)
	require.Equal(t, 0, points[1].AverageHappyScore)
	require.Equal(t, 100, points[2].AverageHappyScore)
	require.Equal(t, "Jumat", points[4].Day)
}
