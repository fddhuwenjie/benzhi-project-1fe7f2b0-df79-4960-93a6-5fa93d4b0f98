package application

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"paperqual/internal/domain"
	"paperqual/internal/evidence"
	"paperqual/internal/store"
)

type Clock func() time.Time

type Service struct {
	repo              *store.Repository
	coord             *Coordinator
	now               Clock
	verificationMu    sync.RWMutex
	verificationCache map[string]evidence.IntegrityReport
}

func NewService(repo *store.Repository) *Service {
	return &Service{repo: repo, coord: NewCoordinator(128), now: time.Now, verificationCache: make(map[string]evidence.IntegrityReport)}
}
func NewServiceWithClock(repo *store.Repository, clock Clock) *Service {
	return &Service{repo: repo, coord: NewCoordinator(128), now: clock, verificationCache: make(map[string]evidence.IntegrityReport)}
}

func validateMeta(meta CommandMeta, create bool) error {
	if strings.TrimSpace(meta.RequestID) == "" {
		return domain.Errorf(domain.CodeValidation, "request_id 不能为空")
	}
	if len(meta.RequestID) > 128 {
		return domain.Errorf(domain.CodeValidation, "request_id 过长")
	}
	if strings.TrimSpace(meta.ActorID) == "" {
		return domain.Errorf(domain.CodeValidation, "X-Actor-ID 不能为空")
	}
	if create {
		if meta.ExpectedRevision != 0 {
			return domain.Errorf(domain.CodeValidation, "创建批次的 expected_revision 必须为 0")
		}
	} else if meta.ExpectedRevision < 1 {
		return domain.Errorf(domain.CodeValidation, "expected_revision 必须大于 0")
	}
	return nil
}

func fingerprint(action string, meta CommandMeta, input any) (string, error) {
	material := struct {
		Action           string `json:"action"`
		ActorID          string `json:"actor_id"`
		ExpectedRevision int64  `json:"expected_revision"`
		Input            any    `json:"input"`
	}{action, meta.ActorID, meta.ExpectedRevision, input}
	digest, _, err := evidence.Digest(material)
	return digest, err
}

func response(v any) (json.RawMessage, error) {
	raw, err := evidence.CanonicalJSON(v)
	return json.RawMessage(raw), err
}

func (s *Service) Create(meta CommandMeta, in CreateBatch) (Result, error) {
	if err := validateMeta(meta, true); err != nil {
		return Result{}, err
	}
	unlock := s.coord.lock(in.BatchID)
	defer unlock()
	fp, err := fingerprint("batch.created", meta, in)
	if err != nil {
		return Result{}, err
	}
	if snap, loadErr := s.repo.Load(in.BatchID); loadErr == nil {
		if entry, ok := snap.Idempotency[meta.RequestID]; ok {
			if entry.Fingerprint != fp {
				return Result{}, domain.WithRevision(domain.Errorf(domain.CodeIdempotency, "request_id 已被不同请求使用"), snap.Batch.Revision)
			}
			return Result{StatusCode: entry.StatusCode, Body: entry.Response, Replayed: true}, nil
		}
		return Result{}, domain.WithRevision(domain.Errorf(domain.CodeValidation, "batch_id 已存在"), snap.Batch.Revision)
	} else if de, ok := loadErr.(*domain.Error); !ok || de.Code != domain.CodeNotFound {
		return Result{}, loadErr
	}
	if meta.ActorID != in.OperatorID {
		return Result{}, domain.Errorf(domain.CodeValidation, "创建者必须与 operator_id 一致")
	}
	batch, err := domain.NewBatch(in.BatchID, in.Title, in.OperatorID, in.ReviewerID, in.Standards, s.now())
	if err != nil {
		return Result{}, err
	}
	body, err := response(BatchView{Batch: *batch, EventSequence: 1})
	if err != nil {
		return Result{}, err
	}
	event := s.event("batch.created", meta, batch.Revision, in)
	_, err = s.repo.Create(store.CommitRequest{Batch: *batch, ExpectedBase: 0, Event: event, RequestID: meta.RequestID, Fingerprint: fp, StatusCode: 201, Response: body})
	if err != nil {
		return Result{}, err
	}
	return Result{StatusCode: 201, Body: body}, nil
}

type mutation func(*domain.TreatmentBatch, *store.Snapshot) (string, any, json.RawMessage, error)

func (s *Service) mutate(batchID, action string, meta CommandMeta, input any, status int, fn mutation) (Result, error) {
	if err := validateMeta(meta, false); err != nil {
		return Result{}, err
	}
	unlock := s.coord.lock(batchID)
	defer unlock()
	fp, err := fingerprint(action, meta, input)
	if err != nil {
		return Result{}, err
	}
	snap, err := s.repo.Load(batchID)
	if err != nil {
		return Result{}, err
	}
	if entry, ok := snap.Idempotency[meta.RequestID]; ok {
		if entry.Fingerprint != fp {
			return Result{}, domain.WithRevision(domain.Errorf(domain.CodeIdempotency, "request_id 已被不同请求使用"), snap.Batch.Revision)
		}
		return Result{StatusCode: entry.StatusCode, Body: entry.Response, Replayed: true}, nil
	}
	if snap.Batch.Revision != meta.ExpectedRevision {
		return Result{}, domain.WithRevision(domain.Errorf(domain.CodeRevisionConflict, "expected_revision 已过期"), snap.Batch.Revision)
	}
	batch := snap.Batch
	eventType, eventData, certificate, err := fn(&batch, &snap)
	if err != nil {
		return Result{}, domain.WithRevision(err, snap.Batch.Revision)
	}
	batch.Revision = snap.Batch.Revision + 1
	body, err := response(BatchView{Batch: batch, EventSequence: snap.Sequence + 1})
	if err != nil {
		return Result{}, err
	}
	event := s.event(eventType, meta, batch.Revision, eventData)
	_, err = s.repo.Commit(store.CommitRequest{Batch: batch, ExpectedBase: snap.Batch.Revision, Event: event, RequestID: meta.RequestID, Fingerprint: fp, StatusCode: status, Response: body, Certificate: certificate})
	if err != nil {
		return Result{}, err
	}
	return Result{StatusCode: status, Body: body}, nil
}

func (s *Service) event(eventType string, meta CommandMeta, revision int64, data any) store.EventPayload {
	raw, _ := response(data)
	return store.EventPayload{EventType: eventType, ActorID: meta.ActorID, RequestID: meta.RequestID, Revision: revision, OccurredAt: s.now().UTC(), Data: raw}
}

func (s *Service) RegisterItem(batchID string, meta CommandMeta, in RegisterItem) (Result, error) {
	return s.mutate(batchID, "item.registered", meta, in, 200, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		if err := b.AddItem(archiveItem(in)); err != nil {
			return "", nil, nil, err
		}
		return "item.registered", in, nil, nil
	})
}

func archiveItem(in RegisterItem) domain.ArchiveItem {
	return domain.ArchiveItem{ItemID: in.ItemID, ShelfMark: in.ShelfMark, PaperType: in.PaperType, BaselineSurfacePH: in.BaselineSurfacePH, BaselineColdExtractPH: in.BaselineColdExtractPH, MeasurementPoints: in.MeasurementPoints, SourceDigest: in.SourceDigest}
}

func (s *Service) RegisterItems(batchID string, meta CommandMeta, in RegisterItems) (Result, error) {
	return s.mutate(batchID, "items.batch_registered", meta, in, 200, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		items := make([]domain.ArchiveItem, 0, len(in.Items))
		for _, item := range in.Items {
			items = append(items, archiveItem(item))
		}
		if err := b.AddItems(items); err != nil {
			return "", nil, nil, err
		}
		return "items.batch_registered", in, nil, nil
	})
}

func (s *Service) FreezeBaseline(batchID string, meta CommandMeta) (Result, error) {
	return s.mutate(batchID, "baseline.frozen", meta, struct{}{}, 200, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		_, digest, err := evidence.BuildBaseline(b)
		if err != nil {
			return "", nil, nil, err
		}
		if err := b.FreezeBaseline(digest); err != nil {
			return "", nil, nil, err
		}
		return "baseline.frozen", map[string]string{"baseline_digest": digest}, nil, nil
	})
}

func (s *Service) SubmitTreatmentRound(batchID string, meta CommandMeta, in SubmitRound) (Result, error) {
	return s.mutate(batchID, "round.submitted", meta, in, 200, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		round := domain.TreatmentRound{RoundID: in.RoundID, RoundKind: in.RoundKind, SubmittedBy: meta.ActorID, StartedAt: in.StartedAt, CompletedAt: in.CompletedAt, Measurements: append([]domain.Measurement(nil), in.Measurements...), EvidenceDigest: in.EvidenceDigest}
		if err := b.SubmitRound(round); err != nil {
			return "", nil, nil, err
		}
		return "round.submitted", in, nil, nil
	})
}

func (s *Service) PreflightTreatmentRound(batchID string, meta CommandMeta, in SubmitRound) (RoundPreflightReport, error) {
	if err := validateMeta(meta, false); err != nil {
		return RoundPreflightReport{}, err
	}
	unlock := s.coord.lock(batchID)
	defer unlock()
	snap, err := s.repo.Load(batchID)
	if err != nil {
		return RoundPreflightReport{}, err
	}
	if snap.Batch.Revision != meta.ExpectedRevision {
		return RoundPreflightReport{}, domain.WithRevision(domain.Errorf(domain.CodeRevisionConflict, "expected_revision 已过期"), snap.Batch.Revision)
	}
	round := domain.TreatmentRound{RoundID: in.RoundID, RoundKind: in.RoundKind, SubmittedBy: meta.ActorID, StartedAt: in.StartedAt, CompletedAt: in.CompletedAt, Measurements: append([]domain.Measurement(nil), in.Measurements...), EvidenceDigest: in.EvidenceDigest}
	preview, err := snap.Batch.PreviewRound(round)
	if err != nil {
		return RoundPreflightReport{}, domain.WithRevision(err, snap.Batch.Revision)
	}
	return RoundPreflightReport{CurrentRevision: snap.Batch.Revision, Baseline: BaselineSummary{Digest: snap.Batch.BaselineDigest, ItemCount: len(snap.Batch.Items), Standards: snap.Batch.Standards}, Preview: preview}, nil
}

func (s *Service) RecordCorrection(batchID string, meta CommandMeta, in RecordCorrection) (Result, error) {
	return s.mutate(batchID, "correction.recorded", meta, in, 200, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		c := domain.Correction{CorrectionID: in.CorrectionID, ItemID: in.ItemID, Reason: in.Reason, Action: in.Action, EvidenceDigest: in.EvidenceDigest, RecordedBy: meta.ActorID, RecordedAt: s.now()}
		if err := b.RecordCorrection(c); err != nil {
			return "", nil, nil, err
		}
		return "correction.recorded", in, nil, nil
	})
}

func (s *Service) RecordCorrections(batchID string, meta CommandMeta, in RecordCorrections) (Result, error) {
	return s.mutate(batchID, "corrections.batch_recorded", meta, in, 200, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		corrections := make([]domain.Correction, 0, len(in.Corrections))
		for _, correction := range in.Corrections {
			cause := correction.Reason
			corrections = append(corrections, domain.Correction{CorrectionID: correction.CorrectionID, ItemID: correction.ItemID, Reason: cause.Description, Cause: &cause, Action: correction.Action, EvidenceDigest: correction.EvidenceDigest, RecordedBy: meta.ActorID, RecordedAt: s.now()})
		}
		if err := b.RecordCorrections(corrections); err != nil {
			return "", nil, nil, err
		}
		return "corrections.batch_recorded", in, nil, nil
	})
}

func (s *Service) StartReview(batchID string, meta CommandMeta, in StartReview) (Result, error) {
	return s.mutate(batchID, "review.started", meta, in, 201, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		if err := b.StartReview(in.ReviewID, meta.ActorID); err != nil {
			return "", nil, nil, err
		}
		return "review.started", b.Review, nil, nil
	})
}

func (s *Service) SubmitReview(batchID string, meta CommandMeta, in SubmitReview) (Result, error) {
	return s.mutate(batchID, "review.signed", meta, in, 200, func(b *domain.TreatmentBatch, _ *store.Snapshot) (string, any, json.RawMessage, error) {
		if err := b.SubmitReview(meta.ActorID, in.Decisions, s.now()); err != nil {
			return "", nil, nil, err
		}
		return "review.signed", b.Review, nil, nil
	})
}

func (s *Service) Decide(batchID string, meta CommandMeta, in FinalDecision) (Result, error) {
	return s.mutate(batchID, "batch.sealed", meta, in, 200, func(b *domain.TreatmentBatch, snap *store.Snapshot) (string, any, json.RawMessage, error) {
		if err := b.Seal(in.Decision, meta.ActorID, s.now()); err != nil {
			return "", nil, nil, err
		}
		b.Revision = snap.Batch.Revision + 1
		b.AuditAnchor = snap.EventAnchor
		envelope, raw, err := evidence.BuildCertificate(b)
		if err != nil {
			return "", nil, nil, err
		}
		b.CertificateDigest = envelope.Digest
		b.Review.CertificateDigest = envelope.Digest
		return "batch.sealed", map[string]string{"decision": in.Decision, "certificate_digest": envelope.Digest}, raw, nil
	})
}

func (s *Service) GetBatch(batchID string) (BatchView, error) {
	snap, err := s.repo.Load(batchID)
	if err != nil {
		return BatchView{}, err
	}
	return BatchView{Batch: snap.Batch, EventSequence: snap.Sequence, EventAnchor: snap.EventAnchor}, nil
}
func (s *Service) Timeline(batchID string) ([]store.TimelineEntry, string, error) {
	if _, err := s.repo.Load(batchID); err != nil {
		return nil, "", err
	}
	return s.repo.Timeline(batchID)
}
func (s *Service) QueryTimeline(batchID string, query TimelineQuery) (store.TimelinePage, error) {
	return s.repo.QueryTimeline(batchID, store.TimelineQuery{Cursor: query.Cursor, Limit: query.Limit, EventType: query.EventType, ActorID: query.ActorID, MinRevision: query.MinRevision, MaxRevision: query.MaxRevision, SnapshotAnchor: query.SnapshotAnchor})
}
func (s *Service) Certificate(batchID string) (json.RawMessage, error) {
	return s.repo.Certificate(batchID)
}
func (s *Service) VerifyCertificate(batchID string) (evidence.IntegrityReport, error) {
	s.verificationMu.RLock()
	cached, ok := s.verificationCache[batchID]
	s.verificationMu.RUnlock()
	if ok {
		return cached, nil
	}
	snap, err := s.repo.Load(batchID)
	if err != nil {
		return evidence.IntegrityReport{}, err
	}
	raw, err := s.repo.Certificate(batchID)
	if err != nil {
		return evidence.IntegrityReport{}, err
	}
	report := evidence.VerifyCertificate(raw, &snap.Batch)
	if !report.Valid {
		return report, domain.WithRevision(domain.Errorf(domain.CodeEvidenceCorrupt, report.Message), snap.Batch.Revision)
	}
	entries, _, err := s.repo.Timeline(batchID)
	if err != nil {
		return report, err
	}
	found := snap.Batch.AuditAnchor == ""
	for _, entry := range entries {
		if entry.Digest == snap.Batch.AuditAnchor {
			found = true
			break
		}
	}
	if !found {
		report.Valid = false
		report.AuditAnchorMatches = false
		report.Message = "证书审计锚点不在事件链中"
		return report, domain.WithRevision(domain.Errorf(domain.CodeEvidenceCorrupt, report.Message), snap.Batch.Revision)
	}
	s.verificationMu.Lock()
	s.verificationCache[batchID] = report
	s.verificationMu.Unlock()
	return report, nil
}

func (s *Service) Ready() error {
	if s.repo == nil {
		return fmt.Errorf("仓储未初始化")
	}
	return nil
}
