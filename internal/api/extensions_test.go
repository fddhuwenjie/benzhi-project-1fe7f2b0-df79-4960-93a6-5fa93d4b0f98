package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/store"
)

const extensionDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBulkRegistrationPreflightCorrectionsAndTimelineAnchor(t *testing.T) {
	server := testServer(t)
	create := map[string]any{"request_id": "create", "expected_revision": 0, "batch_id": "batch-ext", "title": "扩展验收", "operator_id": "operator", "reviewer_id": "reviewer", "standards": map[string]any{"target_surface_ph_min": 7, "target_surface_ph_max": 9, "min_alkaline_reserve_pct": 1, "max_color_delta_e": 3, "sample_ratio": 1}}
	if resp := request(t, server, http.MethodPost, "/api/v1/batches", "operator", create); resp.Code != http.StatusCreated {
		t.Fatalf("创建失败: %d %s", resp.Code, resp.Body.String())
	}
	item := func(id, shelf string) map[string]any {
		return map[string]any{"item_id": id, "shelf_mark": shelf, "paper_type": "机制纸", "baseline_surface_ph": 5, "baseline_cold_extract_ph": 5, "measurement_points": 4, "source_digest": extensionDigest}
	}
	badBulk := map[string]any{"request_id": "items-bad", "expected_revision": 1, "items": []map[string]any{item("item-1", "A-1"), item("item-2", "A-1")}}
	bad := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/items/batch", "operator", badBulk)
	if bad.Code != http.StatusConflict {
		t.Fatalf("重复架位应拒绝: %d %s", bad.Code, bad.Body.String())
	}
	var badBody errorBody
	if err := json.Unmarshal(bad.Body.Bytes(), &badBody); err != nil || len(badBody.Error.Details) == 0 || badBody.Error.Details[0].Code != "duplicate_shelf_mark" {
		t.Fatalf("缺少稳定逐项错误: %s", bad.Body.String())
	}
	viewResp := request(t, server, http.MethodGet, "/api/v1/batches/batch-ext", "", nil)
	var view application.BatchView
	if err := json.Unmarshal(viewResp.Body.Bytes(), &view); err != nil || view.Batch.Revision != 1 || len(view.Batch.Items) != 0 {
		t.Fatalf("失败批量改变了批次: %s", viewResp.Body.String())
	}

	bulk := map[string]any{"request_id": "items-ok", "expected_revision": 1, "items": []map[string]any{item("item-1", "A-1"), item("item-2", "A-2")}}
	first := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/items/batch", "operator", bulk)
	replay := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/items/batch", "operator", bulk)
	if first.Code != http.StatusOK || replay.Header().Get("X-Idempotent-Replay") != "true" || replay.Body.String() != first.Body.String() {
		t.Fatalf("批量登记或幂等重放失败: first=%d replay=%d", first.Code, replay.Code)
	}
	if err := json.Unmarshal(first.Body.Bytes(), &view); err != nil || view.Batch.Revision != 2 || len(view.Batch.Items) != 2 {
		t.Fatalf("批量登记没有只提升一个修订: %s", first.Body.String())
	}

	timelineResp := request(t, server, http.MethodGet, "/api/v1/batches/batch-ext/timeline?limit=1", "", nil)
	var timeline store.TimelinePage
	if err := json.Unmarshal(timelineResp.Body.Bytes(), &timeline); err != nil || timeline.TotalEvents != 2 || timeline.NextCursor == nil {
		t.Fatalf("时间线首屏错误: %s", timelineResp.Body.String())
	}
	freeze := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/baseline", "operator", map[string]any{"request_id": "freeze", "expected_revision": 2})
	if freeze.Code != http.StatusOK {
		t.Fatalf("冻结失败: %s", freeze.Body.String())
	}
	stalePath := fmt.Sprintf("/api/v1/batches/batch-ext/timeline?limit=1&cursor=%d&snapshot_anchor=%s", *timeline.NextCursor, timeline.EventAnchor)
	stale := request(t, server, http.MethodGet, stalePath, "", nil)
	if stale.Code != http.StatusConflict {
		t.Fatalf("旧锚点翻页应冲突: %d %s", stale.Code, stale.Body.String())
	}
	var staleBody errorBody
	_ = json.Unmarshal(stale.Body.Bytes(), &staleBody)
	if staleBody.Error.Code != domain.CodeTimelineChanged {
		t.Fatalf("旧锚点错误码不正确: %s", stale.Body.String())
	}

	measurements := []map[string]any{
		{"item_id": "item-1", "surface_ph": 8, "cold_extract_ph": 8, "alkaline_reserve_pct": 0.5, "color_delta_e": 1, "source_digest": extensionDigest},
		{"item_id": "item-2", "surface_ph": 8, "cold_extract_ph": 8, "alkaline_reserve_pct": 2, "color_delta_e": 1, "source_digest": extensionDigest},
	}
	round := map[string]any{"request_id": "preflight", "expected_revision": 3, "round_id": "round-1", "round_kind": "treatment", "started_at": "2026-01-02T03:00:00Z", "completed_at": "2026-01-02T04:00:00Z", "evidence_digest": extensionDigest, "measurements": measurements}
	preflight := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/treatment-rounds/preflight", "operator", round)
	var report application.RoundPreflightReport
	if preflight.Code != http.StatusOK || json.Unmarshal(preflight.Body.Bytes(), &report) != nil || report.CurrentRevision != 3 || report.Preview.ExpectedStatus != domain.StatusQuarantined || report.Preview.Items[0].FailureCodes[0] != "alkaline_reserve_below_min" {
		t.Fatalf("预检报告错误: %d %s", preflight.Code, preflight.Body.String())
	}
	viewResp = request(t, server, http.MethodGet, "/api/v1/batches/batch-ext", "", nil)
	if err := json.Unmarshal(viewResp.Body.Bytes(), &view); err != nil || view.Batch.Revision != 3 || view.Batch.Status != domain.StatusBaseline || len(view.Batch.Rounds) != 0 {
		t.Fatalf("预检写入了批次: %s", viewResp.Body.String())
	}
	round["request_id"] = "round-submit"
	submitted := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/treatment-rounds", "operator", round)
	if submitted.Code != http.StatusOK {
		t.Fatalf("正式轮次失败: %s", submitted.Body.String())
	}

	badCorrections := map[string]any{"request_id": "corrections-bad", "expected_revision": 4, "corrections": []map[string]any{{"correction_id": "fix-wrong", "item_id": "item-2", "reason": map[string]any{"category": "surface_ph", "description": "错误关联"}, "action": "不执行", "evidence_digest": extensionDigest}}}
	correctionFailure := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/corrections/batch", "operator", badCorrections)
	if correctionFailure.Code != http.StatusUnprocessableEntity {
		t.Fatalf("非法批量纠正应失败: %d %s", correctionFailure.Code, correctionFailure.Body.String())
	}
	validCorrections := map[string]any{"request_id": "corrections-ok", "expected_revision": 4, "corrections": []map[string]any{{"correction_id": "fix-1", "item_id": "item-1", "reason": map[string]any{"category": "alkaline_reserve", "description": "药液浓度不足"}, "action": "调整浓度后定向复测", "evidence_digest": extensionDigest}}}
	corrected := request(t, server, http.MethodPost, "/api/v1/batches/batch-ext/corrections/batch", "operator", validCorrections)
	if corrected.Code != http.StatusOK || json.Unmarshal(corrected.Body.Bytes(), &view) != nil || view.Batch.Revision != 5 || view.Batch.Items[0].CorrectionID != "fix-1" || len(view.Batch.Corrections[0].FailureCodes) != 1 {
		t.Fatalf("批量纠正提交错误: %d %s", corrected.Code, corrected.Body.String())
	}
}
