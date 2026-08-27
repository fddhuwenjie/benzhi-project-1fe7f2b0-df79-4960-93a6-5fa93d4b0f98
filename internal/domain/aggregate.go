package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

func NewBatch(id, title, operatorID, reviewerID string, standards Standards, now time.Time) (*TreatmentBatch, error) {
	for field, value := range map[string]string{"batch_id": id, "operator_id": operatorID, "reviewer_id": reviewerID} {
		if err := ValidateIdentifier(field, value); err != nil {
			return nil, err
		}
	}
	if err := ValidateText("title", title, 256); err != nil {
		return nil, err
	}
	if operatorID == reviewerID {
		return nil, Errorf(CodeIndependence, "operator_id 与 reviewer_id 必须不同")
	}
	if err := ValidateStandards(standards); err != nil {
		return nil, err
	}
	return &TreatmentBatch{BatchID: id, Title: title, OperatorID: operatorID, ReviewerID: reviewerID, Status: StatusDraft, Standards: standards, Revision: 1, CreatedAt: now.UTC(), Items: []ArchiveItem{}, Rounds: []TreatmentRound{}, Corrections: []Correction{}}, nil
}

func (b *TreatmentBatch) EnsureWritable() error {
	if b.Status.Terminal() {
		return Errorf(CodeReadOnly, "终态批次不可再写")
	}
	return nil
}

func (b *TreatmentBatch) AddItem(item ArchiveItem) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusDraft {
		return Errorf(CodeInvalidState, "只能在基线冻结前登记档案件")
	}
	if err := ValidateItem(item); err != nil {
		return err
	}
	if _, ok := b.FindItem(item.ItemID); ok {
		return Errorf(CodeDuplicateItem, "item_id %s 已存在", item.ItemID)
	}
	for _, existing := range b.Items {
		if existing.ShelfMark == item.ShelfMark {
			return Errorf(CodeDuplicateItem, "shelf_mark %s 已存在", item.ShelfMark)
		}
	}
	item.BatchID = b.BatchID
	item.QualificationStatus = "pending"
	item.FailureCodes = []string{}
	b.Items = append(b.Items, item)
	return nil
}

func (b *TreatmentBatch) FreezeBaseline(digest string) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusDraft {
		return Errorf(CodeInvalidState, "当前状态不能冻结基线")
	}
	if len(b.Items) == 0 {
		return Errorf(CodeValidation, "至少登记一件档案后才能冻结基线")
	}
	if !ValidDigest(digest) {
		return Errorf(CodeValidation, "baseline_digest 无效")
	}
	b.BaselineDigest = digest
	b.Status = StatusBaseline
	return nil
}

func EvaluateMeasurement(s Standards, m Measurement) []string {
	codes := make([]string, 0, 3)
	if m.SurfacePH < s.TargetSurfacePHMin {
		codes = append(codes, "surface_ph_below_min")
	}
	if m.SurfacePH > s.TargetSurfacePHMax {
		codes = append(codes, "surface_ph_above_max")
	}
	if m.AlkalineReservePct < s.MinAlkalineReservePct {
		codes = append(codes, "alkaline_reserve_below_min")
	}
	if m.ColorDeltaE > s.MaxColorDeltaE {
		codes = append(codes, "color_delta_e_above_max")
	}
	return codes
}

func (b *TreatmentBatch) SubmitRound(round TreatmentRound) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if round.RoundKind != "treatment" && round.RoundKind != "retest" {
		return Errorf(CodeValidation, "round_kind 必须是 treatment 或 retest")
	}
	if err := ValidateIdentifier("round_id", round.RoundID); err != nil {
		return err
	}
	if err := ValidateIdentifier("submitted_by", round.SubmittedBy); err != nil {
		return err
	}
	if round.SubmittedBy != b.OperatorID {
		return Errorf(CodeValidation, "处理轮次只能由批次实验员提交")
	}
	if !round.CompletedAt.After(round.StartedAt) {
		return Errorf(CodeValidation, "completed_at 必须晚于 started_at")
	}
	if !ValidDigest(round.EvidenceDigest) {
		return Errorf(CodeValidation, "evidence_digest 无效")
	}
	for _, existing := range b.Rounds {
		if existing.RoundID == round.RoundID {
			return Errorf(CodeValidation, "round_id 已存在")
		}
	}
	if round.RoundKind == "treatment" {
		if b.Status != StatusBaseline || len(b.Rounds) != 0 {
			return Errorf(CodeInvalidState, "当前状态不能提交处理轮次")
		}
		if err := b.applyMeasurements(&round, allItemSet(b), false); err != nil {
			return err
		}
	} else {
		if b.Status != StatusQuarantined {
			return Errorf(CodeInvalidState, "只有隔离批次能提交复测")
		}
		expected := failedItemSet(b)
		if err := b.applyMeasurements(&round, expected, true); err != nil {
			return err
		}
	}
	round.BatchID = b.BatchID
	round.RuleSetDigest = b.BaselineDigest
	if len(b.FailedItemIDs()) == 0 {
		round.Result = "qualified"
		b.Status = StatusReviewReady
	} else {
		round.Result = "failed"
		b.Status = StatusQuarantined
	}
	b.Rounds = append(b.Rounds, round)
	return nil
}

func (b *TreatmentBatch) applyMeasurements(round *TreatmentRound, expected map[string]bool, requireCorrections bool) error {
	seen := make(map[string]bool, len(round.Measurements))
	details := make([]ErrorDetail, 0)
	for i := range round.Measurements {
		m := round.Measurements[i]
		if err := ValidateMeasurement(m); err != nil {
			details = append(details, detail(i, m.ItemID, "invalid_measurement", err.Error()))
		}
		if seen[m.ItemID] {
			details = append(details, detail(i, m.ItemID, "duplicate_measurement", "同一轮次不能重复测量同一档案件"))
		} else if !expected[m.ItemID] {
			details = append(details, detail(i, m.ItemID, "non_target_item", "测量包含非本轮目标档案件"))
		}
		seen[m.ItemID] = true
	}
	missing := make([]string, 0)
	for itemID := range expected {
		if !seen[itemID] {
			missing = append(missing, itemID)
		}
		if requireCorrections {
			item, _ := b.FindItem(itemID)
			if item.CorrectionID == "" {
				details = append(details, ErrorDetail{ItemID: itemID, Code: "missing_correction", Message: "异常件尚未记录纠正措施"})
			}
		}
	}
	sort.Strings(missing)
	for _, itemID := range missing {
		details = append(details, ErrorDetail{ItemID: itemID, Code: "missing_target_item", Message: "轮次缺少目标档案件测量"})
	}
	if len(details) > 0 {
		return NewDetailedError(CodeValidation, "轮次目标范围或测量校验失败", details)
	}
	for i := range round.Measurements {
		m := &round.Measurements[i]
		item, _ := b.FindItem(m.ItemID)
		m.FailureCodes = EvaluateMeasurement(b.Standards, *m)
		item.LatestRoundID = round.RoundID
		item.FailureCodes = append([]string(nil), m.FailureCodes...)
		if len(m.FailureCodes) == 0 {
			item.QualificationStatus = "qualified"
		} else {
			item.QualificationStatus = "failed"
			item.CorrectionID = ""
		}
	}
	return nil
}

func (b *TreatmentBatch) RecordCorrection(c Correction) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusQuarantined {
		return Errorf(CodeInvalidState, "只有隔离批次能记录纠正措施")
	}
	item, ok := b.FindItem(c.ItemID)
	if !ok || item.QualificationStatus != "failed" {
		return Errorf(CodeValidation, "纠正措施只能关联当前异常件")
	}
	if err := ValidateIdentifier("correction_id", c.CorrectionID); err != nil {
		return err
	}
	if err := ValidateIdentifier("recorded_by", c.RecordedBy); err != nil {
		return err
	}
	if err := ValidateText("reason", c.Reason, 1000); err != nil {
		return err
	}
	if err := ValidateText("action", c.Action, 2000); err != nil {
		return err
	}
	if c.RecordedBy != b.OperatorID {
		return Errorf(CodeValidation, "纠正措施只能由批次实验员记录")
	}
	if !ValidDigest(c.EvidenceDigest) {
		return Errorf(CodeValidation, "纠正证据摘要无效")
	}
	for _, existing := range b.Corrections {
		if existing.CorrectionID == c.CorrectionID {
			return Errorf(CodeValidation, "correction_id 已存在")
		}
	}
	c.RecordedAt = c.RecordedAt.UTC()
	c.FailureCodes = append([]string(nil), item.FailureCodes...)
	b.Corrections = append(b.Corrections, c)
	item.CorrectionID = c.CorrectionID
	return nil
}

func allItemSet(b *TreatmentBatch) map[string]bool {
	out := map[string]bool{}
	for _, i := range b.Items {
		out[i.ItemID] = true
	}
	return out
}
func failedItemSet(b *TreatmentBatch) map[string]bool {
	out := map[string]bool{}
	for _, i := range b.Items {
		if i.QualificationStatus == "failed" {
			out[i.ItemID] = true
		}
	}
	return out
}

func (b *TreatmentBatch) BaselineMaterial() []byte {
	items := append([]ArchiveItem(nil), b.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ItemID < items[j].ItemID })
	var text strings.Builder
	fmt.Fprintf(&text, "%s|%s|%.6f|%.6f|%.6f|%.6f|%.6f", b.BatchID, b.Title, b.Standards.TargetSurfacePHMin, b.Standards.TargetSurfacePHMax, b.Standards.MinAlkalineReservePct, b.Standards.MaxColorDeltaE, b.Standards.SampleRatio)
	for _, item := range items {
		fmt.Fprintf(&text, "|%s|%s|%s|%.6f|%.6f|%d|%s", item.ItemID, item.ShelfMark, item.PaperType, item.BaselineSurfacePH, item.BaselineColdExtractPH, item.MeasurementPoints, item.SourceDigest)
	}
	return []byte(text.String())
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
