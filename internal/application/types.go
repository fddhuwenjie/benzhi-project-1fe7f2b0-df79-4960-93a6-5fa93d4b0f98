package application

import (
	"encoding/json"
	"time"

	"paperqual/internal/domain"
)

type CommandMeta struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	ActorID          string `json:"actor_id"`
}

type Result struct {
	StatusCode int
	Body       json.RawMessage
	Replayed   bool
}

type CreateBatch struct {
	BatchID    string           `json:"batch_id"`
	Title      string           `json:"title"`
	OperatorID string           `json:"operator_id"`
	ReviewerID string           `json:"reviewer_id"`
	Standards  domain.Standards `json:"standards"`
}

type RegisterItem struct {
	ItemID                string  `json:"item_id"`
	ShelfMark             string  `json:"shelf_mark"`
	PaperType             string  `json:"paper_type"`
	BaselineSurfacePH     float64 `json:"baseline_surface_ph"`
	BaselineColdExtractPH float64 `json:"baseline_cold_extract_ph"`
	MeasurementPoints     int     `json:"measurement_points"`
	SourceDigest          string  `json:"source_digest"`
}

type RegisterItems struct {
	Items []RegisterItem `json:"items"`
}

type SubmitRound struct {
	RoundID        string               `json:"round_id"`
	RoundKind      string               `json:"round_kind"`
	StartedAt      time.Time            `json:"started_at"`
	CompletedAt    time.Time            `json:"completed_at"`
	Measurements   []domain.Measurement `json:"measurements"`
	EvidenceDigest string               `json:"evidence_digest"`
}

type RecordCorrection struct {
	CorrectionID   string `json:"correction_id"`
	ItemID         string `json:"item_id"`
	Reason         string `json:"reason"`
	Action         string `json:"action"`
	EvidenceDigest string `json:"evidence_digest"`
}

type BulkCorrection struct {
	CorrectionID   string                 `json:"correction_id"`
	ItemID         string                 `json:"item_id"`
	Reason         domain.CorrectionCause `json:"reason"`
	Action         string                 `json:"action"`
	EvidenceDigest string                 `json:"evidence_digest"`
}

type RecordCorrections struct {
	Corrections []BulkCorrection `json:"corrections"`
}

type BaselineSummary struct {
	Digest    string           `json:"digest"`
	ItemCount int              `json:"item_count"`
	Standards domain.Standards `json:"standards"`
}

type RoundPreflightReport struct {
	CurrentRevision int64               `json:"current_revision"`
	Baseline        BaselineSummary     `json:"baseline"`
	Preview         domain.RoundPreview `json:"preview"`
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

type StartReview struct {
	ReviewID string `json:"review_id"`
}

type SubmitReview struct {
	Decisions []domain.ReviewItemDecision `json:"item_decisions"`
}

type FinalDecision struct {
	Decision string `json:"decision"`
}

type BatchView struct {
	Batch         domain.TreatmentBatch `json:"batch"`
	EventSequence uint64                `json:"event_sequence"`
	EventAnchor   string                `json:"event_anchor"`
}
