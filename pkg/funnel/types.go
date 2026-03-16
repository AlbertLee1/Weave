package funnel

import "time"

// EditType represents the type of edit operation.
type EditType string

const (
	EditTypeCreate EditType = "CREATE"
	EditTypeModify EditType = "MODIFY"
	EditTypeDelete EditType = "DELETE"
)

// Edit represents a single object edit.
type Edit struct {
	Type       EditType               `json:"type"`
	ObjectType string                 `json:"objectType"`
	PrimaryKey string                 `json:"primaryKey"`
	Properties map[string]interface{} `json:"properties,omitempty"` // for CREATE/MODIFY
}

// EditBatch represents a batch of edits to be applied atomically.
type EditBatch struct {
	ID        string    `json:"id"`
	Edits     []Edit    `json:"edits"`
	UserID    string    `json:"userId"`
	Timestamp time.Time `json:"timestamp"`
}

// ChangeEvent is emitted after edits are applied to notify subscribers.
type ChangeEvent struct {
	ObjectType string   `json:"objectType"`
	PrimaryKey string   `json:"primaryKey"`
	EditType   EditType `json:"editType"`
	Offset     uint64   `json:"offset"` // NATS sequence number
}
