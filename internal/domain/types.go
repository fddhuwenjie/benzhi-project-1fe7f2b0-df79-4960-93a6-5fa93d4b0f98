package domain

import "time"

type BatchStatus string

const (
	StatusDraft       BatchStatus = "draft"
	StatusBaseline    BatchStatus = "baseline_frozen"
	StatusQuarantined BatchStatus = "quarantined"
	StatusReviewReady BatchStatus = "review_ready"
	StatusInReview    BatchStatus = "in_review"
	StatusReleased    BatchStatus = "released"
	StatusRejected    BatchStatus = "rejected"
)

func (s BatchStatus) Terminal() bool { return s == StatusReleased || s == StatusRejected }

type Standards struct {
	TargetSurfacePHMin    float64 `json:"target_surface_ph_min"`
	TargetSurfacePHMax    float64 `json:"target_surface_ph_max"`
	MinAlkalineReservePct float64 `json:"min_alkaline_reserve_pct"`
	MaxColorDeltaE        float64 `json:"max_color_delta_e"`
	SampleRatio           float64 `json:"sample_ratio"`
}

type ArchiveItem struct {
	ItemID                string   `json:"item_id"`
	BatchID               string   `json:"batch_id"`
	ShelfMark             string   `json:"shelf_mark"`
	PaperType             string   `json:"paper_type"`
	BaselineSurfacePH     float64  `json:"baseline_surface_ph"`
	BaselineColdExtractPH float64  `json:"baseline_cold_extract_ph"`
	MeasurementPoints     int      `json:"measurement_points"`
	SourceDigest          string   `json:"source_digest"`
	LatestRoundID         string   `json:"latest_round_id,omitempty"`
	QualificationStatus   string   `json:"qualification_status"`
	FailureCodes          []string `json:"failure_codes"`
	CorrectionID          string   `json:"correction_id,omitempty"`
}

type Measurement struct {
	ItemID             string   `json:"item_id"`
	SurfacePH          float64  `json:"surface_ph"`
	ColdExtractPH      float64  `json:"cold_extract_ph"`
	AlkalineReservePct float64  `json:"alkaline_reserve_pct"`
	ColorDeltaE        float64  `json:"color_delta_e"`
	SourceDigest       string   `json:"source_digest"`
	FailureCodes       []string `json:"failure_codes,omitempty"`
}

type TreatmentRound struct {
	RoundID        string        `json:"round_id"`
	BatchID        string        `json:"batch_id"`
	RoundKind      string        `json:"round_kind"`
	SubmittedBy    string        `json:"submitted_by"`
	StartedAt      time.Time     `json:"started_at"`
	CompletedAt    time.Time     `json:"completed_at"`
	Measurements   []Measurement `json:"measurements"`
	EvidenceDigest string        `json:"evidence_digest"`
	RuleSetDigest  string        `json:"rule_set_digest"`
	Result         string        `json:"result"`
}

type Correction struct {
	CorrectionID   string           `json:"correction_id"`
	ItemID         string           `json:"item_id"`
	Reason         string           `json:"reason"`
	Cause          *CorrectionCause `json:"cause,omitempty"`
	FailureCodes   []string         `json:"failure_codes"`
	Action         string           `json:"action"`
	EvidenceDigest string           `json:"evidence_digest"`
	RecordedBy     string           `json:"recorded_by"`
	RecordedAt     time.Time        `json:"recorded_at"`
}

type CorrectionCause struct {
	Category    string `json:"category"`
	Description string `json:"description"`
}

type ReviewItemDecision struct {
	ItemID   string `json:"item_id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type QualityReview struct {
	ReviewID          string               `json:"review_id"`
	BatchID           string               `json:"batch_id"`
	SampleSeed        string               `json:"sample_seed"`
	SampledItemIDs    []string             `json:"sampled_item_ids"`
	ReviewerID        string               `json:"reviewer_id"`
	ItemDecisions     []ReviewItemDecision `json:"item_decisions"`
	RejectionReasons  []string             `json:"rejection_reasons"`
	OverallDecision   string               `json:"overall_decision"`
	SignedAt          *time.Time           `json:"signed_at,omitempty"`
	CertificateDigest string               `json:"certificate_digest,omitempty"`
}

type TreatmentBatch struct {
	BatchID           string           `json:"batch_id"`
	Title             string           `json:"title"`
	OperatorID        string           `json:"operator_id"`
	ReviewerID        string           `json:"reviewer_id"`
	Status            BatchStatus      `json:"status"`
	Standards         Standards        `json:"standards"`
	BaselineDigest    string           `json:"baseline_digest,omitempty"`
	Revision          int64            `json:"revision"`
	CreatedAt         time.Time        `json:"created_at"`
	SealedAt          *time.Time       `json:"sealed_at,omitempty"`
	Items             []ArchiveItem    `json:"items"`
	Rounds            []TreatmentRound `json:"treatment_rounds"`
	Corrections       []Correction     `json:"corrections"`
	Review            *QualityReview   `json:"review,omitempty"`
	FinalDecision     string           `json:"final_decision,omitempty"`
	CertificateDigest string           `json:"certificate_digest,omitempty"`
	AuditAnchor       string           `json:"audit_anchor,omitempty"`
}

func (b *TreatmentBatch) FindItem(id string) (*ArchiveItem, bool) {
	for i := range b.Items {
		if b.Items[i].ItemID == id {
			return &b.Items[i], true
		}
	}
	return nil, false
}

func (b *TreatmentBatch) FailedItemIDs() []string {
	ids := make([]string, 0)
	for i := range b.Items {
		if b.Items[i].QualificationStatus == "failed" {
			ids = append(ids, b.Items[i].ItemID)
		}
	}
	return ids
}
