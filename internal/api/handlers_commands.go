package api

import (
	"net/http"

	"paperqual/internal/application"
	"paperqual/internal/domain"
)

type createBatchRequest struct {
	RequestID        string           `json:"request_id"`
	ExpectedRevision int64            `json:"expected_revision"`
	BatchID          string           `json:"batch_id"`
	Title            string           `json:"title"`
	OperatorID       string           `json:"operator_id"`
	ReviewerID       string           `json:"reviewer_id"`
	Standards        domain.Standards `json:"standards"`
}
type registerItemRequest struct {
	RequestID             string  `json:"request_id"`
	ExpectedRevision      int64   `json:"expected_revision"`
	ItemID                string  `json:"item_id"`
	ShelfMark             string  `json:"shelf_mark"`
	PaperType             string  `json:"paper_type"`
	BaselineSurfacePH     float64 `json:"baseline_surface_ph"`
	BaselineColdExtractPH float64 `json:"baseline_cold_extract_ph"`
	MeasurementPoints     int     `json:"measurement_points"`
	SourceDigest          string  `json:"source_digest"`
}
type registerItemsRequest struct {
	RequestID        string                     `json:"request_id"`
	ExpectedRevision int64                      `json:"expected_revision"`
	Items            []application.RegisterItem `json:"items"`
}
type roundRequest struct {
	RequestID        string               `json:"request_id"`
	ExpectedRevision int64                `json:"expected_revision"`
	RoundID          string               `json:"round_id"`
	RoundKind        string               `json:"round_kind"`
	StartedAt        timeValue            `json:"started_at"`
	CompletedAt      timeValue            `json:"completed_at"`
	Measurements     []domain.Measurement `json:"measurements"`
	EvidenceDigest   string               `json:"evidence_digest"`
}
type correctionRequest struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	CorrectionID     string `json:"correction_id"`
	ItemID           string `json:"item_id"`
	Reason           string `json:"reason"`
	Action           string `json:"action"`
	EvidenceDigest   string `json:"evidence_digest"`
}
type correctionsRequest struct {
	RequestID        string                       `json:"request_id"`
	ExpectedRevision int64                        `json:"expected_revision"`
	Corrections      []application.BulkCorrection `json:"corrections"`
}
type startReviewRequest struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	ReviewID         string `json:"review_id"`
}
type submitReviewRequest struct {
	RequestID        string                      `json:"request_id"`
	ExpectedRevision int64                       `json:"expected_revision"`
	ItemDecisions    []domain.ReviewItemDecision `json:"item_decisions"`
}
type decisionRequest struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Decision         string `json:"decision"`
}

type commandOutcome struct {
	result application.Result
	err    error
}

func (s *Server) finishCancelable(w http.ResponseWriter, r *http.Request, run func() (application.Result, error)) {
	done := make(chan commandOutcome, 1)
	go func() {
		result, err := run()
		done <- commandOutcome{result: result, err: err}
	}()
	select {
	case outcome := <-done:
		s.finish(w, outcome.result, outcome.err)
	case <-r.Context().Done():
		writeError(w, http.StatusRequestTimeout, "request_canceled", "请求已取消", 0)
	}
}

func (s *Server) HandleCreateBatch(w http.ResponseWriter, r *http.Request) {
	var req createBatchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.Create(meta(r, req.RequestID, req.ExpectedRevision), application.CreateBatch{BatchID: req.BatchID, Title: req.Title, OperatorID: req.OperatorID, ReviewerID: req.ReviewerID, Standards: req.Standards})
	s.finish(w, result, err)
}
func (s *Server) HandleRegisterItem(w http.ResponseWriter, r *http.Request) {
	var req registerItemRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	batchID := r.PathValue("batch_id")
	commandMeta := meta(r, req.RequestID, req.ExpectedRevision)
	input := application.RegisterItem{ItemID: req.ItemID, ShelfMark: req.ShelfMark, PaperType: req.PaperType, BaselineSurfacePH: req.BaselineSurfacePH, BaselineColdExtractPH: req.BaselineColdExtractPH, MeasurementPoints: req.MeasurementPoints, SourceDigest: req.SourceDigest}
	s.finishCancelable(w, r, func() (application.Result, error) {
		return s.service.RegisterItem(batchID, commandMeta, input)
	})
}
func (s *Server) HandleRegisterItems(w http.ResponseWriter, r *http.Request) {
	var req registerItemsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.RegisterItems(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.RegisterItems{Items: req.Items})
	s.finish(w, result, err)
}
func (s *Server) HandleFreezeBaseline(w http.ResponseWriter, r *http.Request) {
	var req emptyCommand
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.FreezeBaseline(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision))
	s.finish(w, result, err)
}
func (s *Server) HandleTreatmentRound(w http.ResponseWriter, r *http.Request) {
	var req roundRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.SubmitTreatmentRound(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.SubmitRound{RoundID: req.RoundID, RoundKind: req.RoundKind, StartedAt: req.StartedAt.Time, CompletedAt: req.CompletedAt.Time, Measurements: req.Measurements, EvidenceDigest: req.EvidenceDigest})
	s.finish(w, result, err)
}
func (s *Server) HandleTreatmentRoundPreflight(w http.ResponseWriter, r *http.Request) {
	var req roundRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	report, err := s.service.PreflightTreatmentRound(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.SubmitRound{RoundID: req.RoundID, RoundKind: req.RoundKind, StartedAt: req.StartedAt.Time, CompletedAt: req.CompletedAt.Time, Measurements: req.Measurements, EvidenceDigest: req.EvidenceDigest})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
func (s *Server) HandleCorrection(w http.ResponseWriter, r *http.Request) {
	var req correctionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.RecordCorrection(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.RecordCorrection{CorrectionID: req.CorrectionID, ItemID: req.ItemID, Reason: req.Reason, Action: req.Action, EvidenceDigest: req.EvidenceDigest})
	s.finish(w, result, err)
}
func (s *Server) HandleCorrections(w http.ResponseWriter, r *http.Request) {
	var req correctionsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.RecordCorrections(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.RecordCorrections{Corrections: req.Corrections})
	s.finish(w, result, err)
}
func (s *Server) HandleStartReview(w http.ResponseWriter, r *http.Request) {
	var req startReviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.StartReview(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.StartReview{ReviewID: req.ReviewID})
	s.finish(w, result, err)
}
func (s *Server) HandleSubmitReview(w http.ResponseWriter, r *http.Request) {
	var req submitReviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.SubmitReview(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.SubmitReview{Decisions: req.ItemDecisions})
	s.finish(w, result, err)
}
func (s *Server) HandleFinalDecision(w http.ResponseWriter, r *http.Request) {
	var req decisionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDomainError(w, err)
		return
	}
	result, err := s.service.Decide(r.PathValue("batch_id"), meta(r, req.RequestID, req.ExpectedRevision), application.FinalDecision{Decision: req.Decision})
	s.finish(w, result, err)
}
func (s *Server) finish(w http.ResponseWriter, result application.Result, err error) {
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeRaw(w, result.StatusCode, result.Body, result.Replayed)
}
