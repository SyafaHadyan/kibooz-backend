package dto

import (
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

type ScanClaimRequest struct {
	StudentID       uuid.UUID           `json:"studentId" validate:"required"`
	TrashType       constants.TrashType `json:"trashType" validate:"required,oneof=ORGANIK ANORGANIK B3"`
	ConfidenceScore float32             `json:"confidenceScore" validate:"gte=0,lte=1"`
	PhotoBase64     string              `json:"photoBase64" validate:"omitempty"`
}

type ScanClaimResponse struct {
	PointsAdded         int `json:"pointsAdded"`
	TotalPoints         int `json:"totalPoints"`
	NewRank             int `json:"newRank"`
	RemainingDailyScans int `json:"remainingDailyScans"`
}

type LeaderboardEntry struct {
	Rank        int     `json:"rank"`
	StudentName string  `json:"studentName"`
	Points      int     `json:"points"`
	AvatarURL   *string `json:"avatarUrl"`
}

type LeaderboardResponse struct {
	Podium   []LeaderboardEntry `json:"podium"`
	Rankings []LeaderboardEntry `json:"rankings"`
}

type AvatarResponse struct {
	AvatarURL string `json:"avatarUrl"`
}

type TrashTypeStat struct {
	TrashType constants.TrashType `json:"trashType"`
	Scans     int                 `json:"scans"`
	Points    int                 `json:"points"`
}

type TrashStatsStudent struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"fullName"`
}

type TrashStatsResponse struct {
	Student             TrashStatsStudent `json:"student"`
	TotalPoints         int               `json:"totalPoints"`
	ClassRank           int               `json:"classRank"`
	TotalScans          int               `json:"totalScans"`
	Breakdown           []TrashTypeStat   `json:"breakdown"`
	TodayScans          int               `json:"todayScans"`
	DailyLimit          int               `json:"dailyLimit"`
	RemainingDailyScans int               `json:"remainingDailyScans"`
}
