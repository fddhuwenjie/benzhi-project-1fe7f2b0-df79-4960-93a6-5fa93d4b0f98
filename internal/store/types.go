package store

import (
	"encoding/json"
	"time"

	"paperqual/internal/domain"
)

type IdempotencyEntry struct {
	Fingerprint string          `json:"fingerprint"`
	StatusCode  int             `json:"status_code"`
	Response    json.RawMessage `json:"response"`
	Revision    int64           `json:"revision"`
	CreatedAt   time.Time       `json:"created_at"`
}

type Snapshot struct {
	Batch       domain.TreatmentBatch       `json:"batch"`
	Sequence    uint64                      `json:"event_sequence"`
	EventAnchor string                      `json:"event_anchor"`
	Idempotency map[string]IdempotencyEntry `json:"idempotency"`
	Certificate json.RawMessage             `json:"certificate,omitempty"`
}

type EventPayload struct {
	EventType  string          `json:"event_type"`
	ActorID    string          `json:"actor_id"`
	RequestID  string          `json:"request_id"`
	Revision   int64           `json:"revision"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type EventFrame struct {
	Sequence       uint64          `json:"sequence"`
	PreviousDigest string          `json:"previous_digest"`
	PayloadDigest  string          `json:"payload_digest"`
	Payload        json.RawMessage `json:"payload"`
	FrameDigest    string          `json:"frame_digest"`
}

type TimelineEntry struct {
	Sequence uint64       `json:"sequence"`
	Digest   string       `json:"digest"`
	Event    EventPayload `json:"event"`
}

type TimelineQuery struct {
	Cursor         *uint64
	Limit          int
	EventType      string
	ActorID        string
	MinRevision    *int64
	MaxRevision    *int64
	SnapshotAnchor string
}

type TimelinePage struct {
	Events          []TimelineEntry `json:"events"`
	EventAnchor     string          `json:"event_anchor"`
	TotalEvents     uint64          `json:"total_events"`
	FilteredEvents  uint64          `json:"filtered_events"`
	FirstSequence   uint64          `json:"first_sequence,omitempty"`
	LastSequence    uint64          `json:"last_sequence,omitempty"`
	NextCursor      *uint64         `json:"next_cursor,omitempty"`
	CurrentRevision int64           `json:"current_revision"`
}

type CommitRequest struct {
	Batch        domain.TreatmentBatch
	ExpectedBase int64
	Event        EventPayload
	RequestID    string
	Fingerprint  string
	StatusCode   int
	Response     json.RawMessage
	Certificate  json.RawMessage
}
