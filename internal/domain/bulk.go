package domain

import (
	"fmt"
	"sort"
)

const MaxBulkItems = 100

func detail(index int, itemID, code, message string) ErrorDetail {
	i := index
	return ErrorDetail{Index: &i, ItemID: itemID, Code: code, Message: message}
}

// AddItems validates the complete request before changing the aggregate.
func (b *TreatmentBatch) AddItems(items []ArchiveItem) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusDraft {
		return Errorf(CodeInvalidState, "只能在基线冻结前登记档案件")
	}
	if len(items) == 0 || len(items) > MaxBulkItems {
		return Errorf(CodeBulkItemsInvalid, "items 必须包含 1 到 %d 件档案", MaxBulkItems)
	}

	existingIDs := make(map[string]bool, len(b.Items))
	existingShelves := make(map[string]bool, len(b.Items))
	for _, item := range b.Items {
		existingIDs[item.ItemID] = true
		existingShelves[item.ShelfMark] = true
	}
	requestIDs := make(map[string]bool, len(items))
	requestShelves := make(map[string]bool, len(items))
	details := make([]ErrorDetail, 0)
	for i, item := range items {
		if err := ValidateItem(item); err != nil {
			details = append(details, detail(i, item.ItemID, "invalid_item", err.Error()))
		}
		if existingIDs[item.ItemID] || requestIDs[item.ItemID] {
			details = append(details, detail(i, item.ItemID, "duplicate_item_id", fmt.Sprintf("item_id %s 已存在或在请求内重复", item.ItemID)))
		}
		if existingShelves[item.ShelfMark] || requestShelves[item.ShelfMark] {
			details = append(details, detail(i, item.ItemID, "duplicate_shelf_mark", fmt.Sprintf("shelf_mark %s 已存在或在请求内重复", item.ShelfMark)))
		}
		requestIDs[item.ItemID] = true
		requestShelves[item.ShelfMark] = true
	}
	if len(details) > 0 {
		code := CodeBulkItemsInvalid
		for _, itemDetail := range details {
			if itemDetail.Code == "duplicate_item_id" || itemDetail.Code == "duplicate_shelf_mark" {
				code = CodeDuplicateItem
				break
			}
		}
		return NewDetailedError(code, "批量登记校验失败", details)
	}
	for _, item := range items {
		item.BatchID = b.BatchID
		item.QualificationStatus = "pending"
		item.FailureCodes = []string{}
		b.Items = append(b.Items, item)
	}
	return nil
}

// RecordCorrections requires exact coverage of every abnormal item that has no correction yet.
func (b *TreatmentBatch) RecordCorrections(corrections []Correction) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusQuarantined {
		return Errorf(CodeInvalidState, "只有隔离批次能记录纠正措施")
	}
	if len(corrections) > MaxBulkItems {
		return Errorf(CodeBulkCorrectionsInvalid, "corrections 不能超过 %d 项纠正记录", MaxBulkItems)
	}

	pending := map[string]bool{}
	for i := range b.Items {
		if b.Items[i].QualificationStatus == "failed" && b.Items[i].CorrectionID == "" {
			pending[b.Items[i].ItemID] = true
		}
	}
	existingCorrectionIDs := map[string]bool{}
	for _, correction := range b.Corrections {
		existingCorrectionIDs[correction.CorrectionID] = true
	}
	seenItems := map[string]bool{}
	seenCorrections := map[string]bool{}
	details := make([]ErrorDetail, 0)
	for i := range corrections {
		c := &corrections[i]
		if err := ValidateIdentifier("correction_id", c.CorrectionID); err != nil {
			details = append(details, detail(i, c.ItemID, "invalid_correction_id", err.Error()))
		}
		if err := ValidateIdentifier("recorded_by", c.RecordedBy); err != nil {
			details = append(details, detail(i, c.ItemID, "invalid_recorded_by", err.Error()))
		} else if c.RecordedBy != b.OperatorID {
			details = append(details, detail(i, c.ItemID, "actor_not_operator", "纠正措施只能由批次实验员记录"))
		}
		if err := ValidateText("action", c.Action, 2000); err != nil {
			details = append(details, detail(i, c.ItemID, "invalid_action", err.Error()))
		}
		if !ValidDigest(c.EvidenceDigest) {
			details = append(details, detail(i, c.ItemID, "invalid_evidence_digest", "纠正证据摘要无效"))
		}
		if c.Cause == nil {
			details = append(details, detail(i, c.ItemID, "missing_reason", "reason 必须包含 category 和 description"))
		} else {
			if err := ValidateText("reason.description", c.Cause.Description, 1000); err != nil {
				details = append(details, detail(i, c.ItemID, "invalid_reason", err.Error()))
			}
			if !validFailureCategory(c.Cause.Category) {
				details = append(details, detail(i, c.ItemID, "invalid_reason_category", "reason.category 必须是 surface_ph、alkaline_reserve 或 color_delta_e"))
			}
			c.Reason = c.Cause.Description
		}
		if existingCorrectionIDs[c.CorrectionID] || seenCorrections[c.CorrectionID] {
			details = append(details, detail(i, c.ItemID, "duplicate_correction_id", "correction_id 已存在或在请求内重复"))
		}
		if seenItems[c.ItemID] {
			details = append(details, detail(i, c.ItemID, "duplicate_item_id", "同一异常件不能重复登记纠正记录"))
		}
		item, ok := b.FindItem(c.ItemID)
		if !ok || item.QualificationStatus != "failed" {
			details = append(details, detail(i, c.ItemID, "non_abnormal_item", "纠正记录关联了非当前异常件"))
		} else if item.CorrectionID != "" {
			details = append(details, detail(i, c.ItemID, "correction_already_recorded", "异常件已经完成纠正登记"))
		} else {
			c.FailureCodes = append([]string(nil), item.FailureCodes...)
			if c.Cause != nil && validFailureCategory(c.Cause.Category) && !categoryMatches(c.Cause.Category, item.FailureCodes) {
				details = append(details, detail(i, c.ItemID, "reason_category_mismatch", "纠正原因类别与原始失败码不匹配"))
			}
		}
		seenItems[c.ItemID] = true
		seenCorrections[c.CorrectionID] = true
	}
	missing := make([]string, 0)
	for itemID := range pending {
		if !seenItems[itemID] {
			missing = append(missing, itemID)
		}
	}
	sort.Strings(missing)
	for _, itemID := range missing {
		details = append(details, ErrorDetail{ItemID: itemID, Code: "missing_correction", Message: "当前异常件缺少纠正记录"})
	}
	if len(corrections) == 0 && len(pending) == 0 {
		return Errorf(CodeBulkCorrectionsInvalid, "当前没有待登记纠正的异常件")
	}
	if len(details) > 0 {
		return NewDetailedError(CodeBulkCorrectionsInvalid, "批量纠正校验失败", details)
	}
	for _, c := range corrections {
		c.RecordedAt = c.RecordedAt.UTC()
		c.FailureCodes = append([]string(nil), c.FailureCodes...)
		b.Corrections = append(b.Corrections, c)
		item, _ := b.FindItem(c.ItemID)
		item.CorrectionID = c.CorrectionID
	}
	return nil
}

func validFailureCategory(category string) bool {
	return category == "surface_ph" || category == "alkaline_reserve" || category == "color_delta_e"
}

func categoryMatches(category string, failureCodes []string) bool {
	for _, code := range failureCodes {
		switch category {
		case "surface_ph":
			if code == "surface_ph_below_min" || code == "surface_ph_above_max" {
				return true
			}
		case "alkaline_reserve":
			if code == "alkaline_reserve_below_min" {
				return true
			}
		case "color_delta_e":
			if code == "color_delta_e_above_max" {
				return true
			}
		}
	}
	return false
}
