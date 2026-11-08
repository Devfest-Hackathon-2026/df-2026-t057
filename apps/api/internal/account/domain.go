// Package account handles data export and resumable account deletion.
package account

import "time"

type Status string

const (
	Pending  Status = "pending"
	Complete Status = "complete"
)

type Job struct {
	UID         string     `json:"uid" firestore:"uid"`
	Status      Status     `json:"status" firestore:"status"`
	ReceiptHash string     `json:"receiptHash" firestore:"receiptHash"`
	ReadyAt     time.Time  `json:"readyAt" firestore:"readyAt"`
	UpdatedAt   time.Time  `json:"updatedAt" firestore:"updatedAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty" firestore:"expiresAt,omitempty"`
}
