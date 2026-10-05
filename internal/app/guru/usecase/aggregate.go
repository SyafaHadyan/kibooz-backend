package usecase

import (
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

var weekdayNames = []string{"Senin", "Selasa", "Rabu", "Kamis", "Jumat"}

// dayMood is the final mood of one student on one local calendar day
type dayMood struct {
	Day     string
	Student uuid.UUID
	Mood    constants.Mood
}

type dayStudent struct {
	day     string
	student uuid.UUID
}

// latestPerStudentPerDay keeps only the newest log of each student for each local day
func latestPerStudentPerDay(records []repository.MoodRecord, loc *time.Location) []dayMood {
	latest := make(map[dayStudent]repository.MoodRecord, len(records))

	for _, record := range records {
		key := dayStudent{
			day:     record.RecordedAt.In(loc).Format(time.DateOnly),
			student: record.StudentID,
		}

		current, exists := latest[key]
		if !exists || !record.RecordedAt.Before(current.RecordedAt) {
			latest[key] = record
		}
	}

	result := make([]dayMood, 0, len(latest))
	for key, record := range latest {
		result = append(result, dayMood{Day: key.day, Student: key.student, Mood: record.MoodType})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Day != result[j].Day {
			return result[i].Day < result[j].Day
		}

		return result[i].Student.String() < result[j].Student.String()
	})

	return result
}

func countMoods(items []dayMood) map[constants.Mood]int {
	counts := make(map[constants.Mood]int, len(constants.Moods))
	for _, item := range items {
		counts[item.Mood]++
	}

	return counts
}

// dominantMood returns the most frequent mood, preferring the chart order on ties
func dominantMood(counts map[constants.Mood]int) *constants.Mood {
	var best *constants.Mood

	bestCount := 0

	for _, mood := range constants.Moods {
		if counts[mood] > bestCount {
			bestCount = counts[mood]
			best = new(constants.Mood)
			*best = mood
		}
	}

	return best
}

// donutSlices converts counts to whole percentages that always add up to 100 when there is data
func donutSlices(counts map[constants.Mood]int) []dto.DonutSlice {
	total := 0
	for _, mood := range constants.Moods {
		total += counts[mood]
	}

	type share struct {
		mood      constants.Mood
		floor     int
		remainder float64
	}

	shares := make([]share, 0, len(constants.Moods))
	assigned := 0

	for _, mood := range constants.Moods {
		exact := 0.0
		if total > 0 {
			exact = float64(counts[mood]) * 100 / float64(total)
		}

		whole := int(math.Floor(exact))
		assigned += whole
		shares = append(shares, share{mood: mood, floor: whole, remainder: exact - float64(whole)})
	}

	if total > 0 {
		order := make([]int, len(shares))
		for i := range order {
			order[i] = i
		}

		sort.SliceStable(order, func(a, b int) bool {
			return shares[order[a]].remainder > shares[order[b]].remainder
		})

		for i := 0; assigned < 100; i++ {
			shares[order[i%len(order)]].floor++
			assigned++
		}
	}

	slices := make([]dto.DonutSlice, 0, len(shares))
	for _, item := range shares {
		slices = append(slices, dto.DonutSlice{
			Mood:       item.mood,
			Percentage: item.floor,
			ColorHex:   constants.MoodColors[item.mood],
		})
	}

	sort.SliceStable(slices, func(a, b int) bool {
		return slices[a].Percentage > slices[b].Percentage
	})

	return slices
}

// weeklyTrend returns the share of happy students for Monday to Friday of the week that starts at weekStart
func weeklyTrend(items []dayMood, weekStart time.Time, loc *time.Location) []dto.TrendPoint {
	perDay := make(map[string][]dayMood, len(weekdayNames))
	for _, item := range items {
		perDay[item.Day] = append(perDay[item.Day], item)
	}

	startLocal := weekStart.In(loc)
	points := make([]dto.TrendPoint, 0, len(weekdayNames))

	for i, name := range weekdayNames {
		day := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day()+i, 0, 0, 0, 0, loc)
		entries := perDay[day.Format(time.DateOnly)]

		score := 0

		if len(entries) > 0 {
			happy := 0

			for _, entry := range entries {
				if entry.Mood == constants.MoodSenang {
					happy++
				}
			}

			score = int(math.Round(float64(happy) * 100 / float64(len(entries))))
		}

		points = append(points, dto.TrendPoint{Day: name, AverageHappyScore: score})
	}

	return points
}
