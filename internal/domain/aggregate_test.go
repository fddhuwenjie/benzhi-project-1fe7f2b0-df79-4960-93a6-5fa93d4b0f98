package domain

import (
	"reflect"
	"testing"
	"time"
)

const testDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func newTestBatch(t *testing.T) *TreatmentBatch {
	t.Helper()
	b, err := NewBatch("batch-1", "测试批次", "operator", "reviewer", Standards{TargetSurfacePHMin: 7, TargetSurfacePHMax: 9.5, MinAlkalineReservePct: 1.5, MaxColorDeltaE: 3, SampleRatio: 0.5}, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []ArchiveItem{
		{ItemID: "item-1", ShelfMark: "A-1", PaperType: "机制纸", BaselineSurfacePH: 5, BaselineColdExtractPH: 5.1, MeasurementPoints: 4, SourceDigest: testDigest},
		{ItemID: "item-2", ShelfMark: "A-2", PaperType: "竹纸", BaselineSurfacePH: 5.2, BaselineColdExtractPH: 5.3, MeasurementPoints: 4, SourceDigest: testDigest},
	} {
		if err := b.AddItem(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.FreezeBaseline(Digest(b.BaselineMaterial())); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestQuarantineCorrectionAndScopedRetest(t *testing.T) {
	b := newTestBatch(t)
	round := TreatmentRound{RoundID: "round-1", RoundKind: "treatment", SubmittedBy: "operator", StartedAt: time.Unix(200, 0), CompletedAt: time.Unix(300, 0), EvidenceDigest: testDigest, Measurements: []Measurement{
		{ItemID: "item-1", SurfacePH: 6.5, ColdExtractPH: 6.7, AlkalineReservePct: 1, ColorDeltaE: 1, SourceDigest: testDigest},
		{ItemID: "item-2", SurfacePH: 8, ColdExtractPH: 7.8, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: testDigest},
	}}
	if err := b.SubmitRound(round); err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusQuarantined || !reflect.DeepEqual(b.FailedItemIDs(), []string{"item-1"}) {
		t.Fatalf("异常隔离结果错误: status=%s failed=%v", b.Status, b.FailedItemIDs())
	}
	retest := TreatmentRound{RoundID: "round-2", RoundKind: "retest", SubmittedBy: "operator", StartedAt: time.Unix(400, 0), CompletedAt: time.Unix(500, 0), EvidenceDigest: testDigest, Measurements: []Measurement{{ItemID: "item-1", SurfacePH: 8, ColdExtractPH: 7.8, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: testDigest}}}
	if err := b.SubmitRound(retest); err == nil {
		t.Fatal("缺少纠正措施时不应允许复测")
	}
	if err := b.RecordCorrection(Correction{CorrectionID: "fix-1", ItemID: "item-1", Reason: "药液浓度偏低", Action: "调整浓度后定向复测", EvidenceDigest: testDigest, RecordedBy: "operator", RecordedAt: time.Unix(350, 0)}); err != nil {
		t.Fatal(err)
	}
	retest.Measurements = append(retest.Measurements, Measurement{ItemID: "item-2", SurfacePH: 8, ColdExtractPH: 8, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: testDigest})
	if err := b.SubmitRound(retest); err == nil {
		t.Fatal("复测不应扩大到正常件")
	}
	retest.Measurements = retest.Measurements[:1]
	if err := b.SubmitRound(retest); err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusReviewReady || len(b.FailedItemIDs()) != 0 {
		t.Fatalf("复测合格后未恢复抽检资格: %s", b.Status)
	}
}

func TestDeterministicReviewAndDecisionRules(t *testing.T) {
	b := newTestBatch(t)
	measurements := []Measurement{{ItemID: "item-1", SurfacePH: 8, ColdExtractPH: 8, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: testDigest}, {ItemID: "item-2", SurfacePH: 8.2, ColdExtractPH: 8, AlkalineReservePct: 2.2, ColorDeltaE: 1, SourceDigest: testDigest}}
	if err := b.SubmitRound(TreatmentRound{RoundID: "round-1", RoundKind: "treatment", SubmittedBy: "operator", StartedAt: time.Unix(200, 0), CompletedAt: time.Unix(300, 0), EvidenceDigest: testDigest, Measurements: measurements}); err != nil {
		t.Fatal(err)
	}
	seed1, ids1 := DeterministicSample(b)
	seed2, ids2 := DeterministicSample(b)
	if seed1 != seed2 || !reflect.DeepEqual(ids1, ids2) || len(ids1) != 1 {
		t.Fatalf("抽样不确定: %s %v / %s %v", seed1, ids1, seed2, ids2)
	}
	if err := b.StartReview("review-1", "operator"); err == nil {
		t.Fatal("实验员不应复核本人提交的数据")
	}
	if err := b.StartReview("review-1", "reviewer"); err != nil {
		t.Fatal(err)
	}
	decisions := []ReviewItemDecision{{ItemID: ids1[0], Decision: "reject", Reason: "原始记录与复测读数不一致"}}
	if err := b.SubmitReview("reviewer", decisions, time.Unix(400, 0)); err != nil {
		t.Fatal(err)
	}
	if err := b.Seal("release", "reviewer", time.Unix(500, 0)); err == nil {
		t.Fatal("抽检驳回后不应允许放行")
	}
	if err := b.Seal("reject", "reviewer", time.Unix(500, 0)); err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusRejected {
		t.Fatalf("拒绝终态错误: %s", b.Status)
	}
	if err := b.RecordCorrection(Correction{}); err == nil {
		t.Fatal("终态批次应只读")
	}
}
