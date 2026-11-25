package dto

import (
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID            uuid.UUID  `json:"id"`
	Kind          string     `json:"kind"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	ReferenceType *string    `json:"referenceType"`
	ReferenceID   *uuid.UUID `json:"referenceId"`
	ReadAt        *time.Time `json:"readAt"`
	CreatedAt     time.Time  `json:"createdAt"`
}

type NotificationList struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unreadCount"`
	PageInfo
}
