package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestAddItemsIsAtomicAndReportsRequestPosition(t *testing.T) {
	b, err := NewBatch("batch-bulk", "批量登记", "operator", "reviewer", Standards{TargetSurfacePHMin: 7, TargetSurfacePHMax: 9, MinAlkalineReservePct: 1, MaxColorDeltaE: 3, SampleRatio: 1}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddItem(ArchiveItem{ItemID: "existing", ShelfMark: "A-1", PaperType: "机制纸", BaselineSurfacePH: 5, BaselineColdExtractPH: 5, MeasurementPoints: 3, SourceDigest: testDigest}); err != nil {
		t.Fatal(err)
	}
	items := []ArchiveItem{
		{ItemID: "item-1", ShelfMark: "A-2", PaperType: "机制纸", BaselineSurfacePH: 5, BaselineColdExtractPH: 5, MeasurementPoints: 3, SourceDigest: testDigest},
		{ItemID: "item-2", ShelfMark: "A-3", PaperType: "竹纸", BaselineSurfacePH: 5, BaselineColdExtractPH: 5, MeasurementPoints: 3, SourceDigest: testDigest},
		{ItemID: "item-3", ShelfMark: "A-1", PaperType: "竹纸", BaselineSurfacePH: 5, BaselineColdExtractPH: 5, MeasurementPoints: 3, SourceDigest: testDigest},
	}
	err = b.AddItems(items)
	de, ok := err.(*Error)
	if !ok || de.Code != CodeDuplicateItem || len(de.Details) != 1 || de.Details[0].Index == nil || *de.Details[0].Index != 2 || de.Details[0].ItemID != "item-3" || de.Details[0].Code != "duplicate_shelf_mark" {
		t.Fatalf("批量错误明细不正确: %#v", err)
	}
	if len(b.Items) != 1 {
		t.Fatalf("失败批量发生了部分写入: %d", len(b.Items))
	}
}

func TestBulkCorrectionsExactCoverageAndFailureCodeRetention(t *testing.T) {
	b := newTestBatch(t)
	round := TreatmentRound{RoundID: "round-failed", RoundKind: "treatment", SubmittedBy: "operator", StartedAt: time.Unix(200, 0), CompletedAt: time.Unix(300, 0), EvidenceDigest: testDigest, Measurements: []Measurement{
		{ItemID: "item-1", SurfacePH: 8, ColdExtractPH: 8, AlkalineReservePct: 0.5, ColorDeltaE: 1, SourceDigest: testDigest},
		{ItemID: "item-2", SurfacePH: 6, ColdExtractPH: 6, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: testDigest},
	}}
	if err := b.SubmitRound(round); err != nil {
		t.Fatal(err)
	}
	emptyErr, ok := b.RecordCorrections(nil).(*Error)
	if !ok || len(emptyErr.Details) != 2 || emptyErr.Details[0].Code != "missing_correction" {
		t.Fatalf("空批量未列出全部未覆盖异常件: %#v", emptyErr)
	}
	bad := []Correction{{CorrectionID: "fix-1", ItemID: "item-1", Cause: &CorrectionCause{Category: "alkaline_reserve", Description: "药液浓度不足"}, Action: "调整浓度", EvidenceDigest: testDigest, RecordedBy: "operator", RecordedAt: time.Unix(400, 0)}, {CorrectionID: "fix-good", ItemID: "not-found", Cause: &CorrectionCause{Category: "surface_ph", Description: "错误关联"}, Action: "无", EvidenceDigest: testDigest, RecordedBy: "operator", RecordedAt: time.Unix(400, 0)}}
	err := b.RecordCorrections(bad)
	de, ok := err.(*Error)
	if !ok || de.Code != CodeBulkCorrectionsInvalid {
		t.Fatalf("应返回批量纠正错误: %#v", err)
	}
	codes := map[string]bool{}
	for _, detail := range de.Details {
		codes[detail.Code] = true
	}
	if !codes["non_abnormal_item"] || !codes["missing_correction"] || len(b.Corrections) != 0 {
		t.Fatalf("未同时报告非法关联和遗漏，或发生部分写入: %+v", de.Details)
	}
	valid := []Correction{
		{CorrectionID: "fix-1", ItemID: "item-1", Cause: &CorrectionCause{Category: "alkaline_reserve", Description: "药液浓度不足"}, Action: "调整浓度", EvidenceDigest: testDigest, RecordedBy: "operator", RecordedAt: time.Unix(400, 0)},
		{CorrectionID: "fix-2", ItemID: "item-2", Cause: &CorrectionCause{Category: "surface_ph", Description: "喷涂覆盖不足"}, Action: "重新喷涂", EvidenceDigest: testDigest, RecordedBy: "operator", RecordedAt: time.Unix(400, 0)},
	}
	if err := b.RecordCorrections(valid); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Corrections[0].FailureCodes, []string{"alkaline_reserve_below_min"}) || b.Items[0].CorrectionID != "fix-1" || b.Items[1].CorrectionID != "fix-2" {
		t.Fatalf("纠正记录未保留原始失败码或未关联异常件: %+v", b.Corrections)
	}
}

func TestRoundPreviewUsesProductionRulesWithoutMutation(t *testing.T) {
	b := newTestBatch(t)
	round := TreatmentRound{RoundID: "preview", RoundKind: "treatment", SubmittedBy: "operator", StartedAt: time.Unix(200, 0), CompletedAt: time.Unix(300, 0), EvidenceDigest: testDigest, Measurements: []Measurement{
		{ItemID: "item-1", SurfacePH: 8, ColdExtractPH: 8, AlkalineReservePct: 0.5, ColorDeltaE: 1, SourceDigest: testDigest},
		{ItemID: "item-2", SurfacePH: 8, ColdExtractPH: 8, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: testDigest},
	}}
	preview, err := b.PreviewRound(round)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExpectedStatus != StatusQuarantined || preview.Items[0].FailureCodes[0] != "alkaline_reserve_below_min" {
		t.Fatalf("预检判定不正确: %+v", preview)
	}
	if b.Status != StatusBaseline || len(b.Rounds) != 0 || b.Items[0].QualificationStatus != "pending" {
		t.Fatalf("预检修改了原聚合: %+v", b)
	}
}
