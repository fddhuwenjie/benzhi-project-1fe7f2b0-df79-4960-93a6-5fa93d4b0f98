package certificatecache_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/evidence"
	"paperqual/internal/store"
)

func TestCachedCertificateVerificationDetectsResourceChange(t *testing.T) {
	root := t.TempDir()
	repo, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithClock(repo, func() time.Time {
		return time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	})
	mustSucceed := func(_ application.Result, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	batchID := "certificate-cache-batch"
	digest := strings.Repeat("a", 64)
	standards := domain.Standards{
		TargetSurfacePHMin:    7,
		TargetSurfacePHMax:    9,
		MinAlkalineReservePct: 1,
		MaxColorDeltaE:        3,
		SampleRatio:           1,
	}

	mustSucceed(service.Create(meta("create", 0, "operator"), application.CreateBatch{
		BatchID: batchID, Title: "证书缓存失效复现", OperatorID: "operator", ReviewerID: "reviewer", Standards: standards,
	}))
	mustSucceed(service.RegisterItem(batchID, meta("item", 1, "operator"), application.RegisterItem{
		ItemID: "item-1", ShelfMark: "A-1", PaperType: "机制纸", BaselineSurfacePH: 5.2,
		BaselineColdExtractPH: 5.1, MeasurementPoints: 5, SourceDigest: digest,
	}))
	mustSucceed(service.FreezeBaseline(batchID, meta("freeze", 2, "operator")))
	mustSucceed(service.SubmitTreatmentRound(batchID, meta("round", 3, "operator"), application.SubmitRound{
		RoundID: "round-1", RoundKind: "treatment",
		StartedAt: time.Date(2026, 8, 27, 7, 0, 0, 0, time.UTC), CompletedAt: time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC),
		EvidenceDigest: digest,
		Measurements:   []domain.Measurement{{ItemID: "item-1", SurfacePH: 8, ColdExtractPH: 7.8, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: digest}},
	}))
	mustSucceed(service.StartReview(batchID, meta("review", 4, "reviewer"), application.StartReview{ReviewID: "review-1"}))
	mustSucceed(service.SubmitReview(batchID, meta("sign", 5, "reviewer"), application.SubmitReview{
		Decisions: []domain.ReviewItemDecision{{ItemID: "item-1", Decision: "accept"}},
	}))
	mustSucceed(service.Decide(batchID, meta("decision", 6, "reviewer"), application.FinalDecision{Decision: "release"}))

	initial, err := service.VerifyCertificate(batchID)
	if err != nil || !initial.Valid {
		t.Fatalf("初次证书校验应通过: report=%+v err=%v", initial, err)
	}

	snapshotPath := filepath.Join(repo.Root(), "snapshots", batchID+".json")
	rawSnapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot store.Snapshot
	if err := json.Unmarshal(rawSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	var envelope evidence.Envelope
	if err := json.Unmarshal(snapshot.Certificate, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Certificate.Title = "被替换且摘要未更新的证书"
	snapshot.Certificate, err = evidence.CanonicalJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	rawSnapshot, err = evidence.CanonicalJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshotPath, rawSnapshot, 0o640); err != nil {
		t.Fatal(err)
	}

	freshReport, freshErr := application.NewService(repo).VerifyCertificate(batchID)
	if freshErr == nil || freshReport.Valid {
		t.Fatalf("复现夹具无效，新服务未识别被替换的证书: report=%+v err=%v", freshReport, freshErr)
	}
	cachedReport, cachedErr := service.VerifyCertificate(batchID)
	if cachedErr == nil || cachedReport.Valid {
		t.Fatalf("缓存复用后仍报告证书有效: report=%+v err=%v", cachedReport, cachedErr)
	}
}

func meta(requestID string, revision int64, actor string) application.CommandMeta {
	return application.CommandMeta{RequestID: requestID, ExpectedRevision: revision, ActorID: actor}
}
