package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"paperqual/internal/application"
	"paperqual/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	repo, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(application.NewService(repo))
}

func request(t *testing.T, server http.Handler, method, path, actor string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if text, ok := body.(string); ok {
		raw = []byte(text)
	} else if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if actor != "" {
		req.Header.Set("X-Actor-ID", actor)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	return recorder
}

func TestStrictJSONRevisionAndIdempotentReplay(t *testing.T) {
	server := testServer(t)
	bad := request(t, server, http.MethodPost, "/api/v1/batches", "operator", `{"request_id":"bad","expected_revision":0,"unknown":true}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应返回 400，实际 %d: %s", bad.Code, bad.Body.String())
	}
	create := map[string]any{"request_id": "create-1", "expected_revision": 0, "batch_id": "batch-1", "title": "测试", "operator_id": "operator", "reviewer_id": "reviewer", "standards": map[string]any{"target_surface_ph_min": 7, "target_surface_ph_max": 9, "min_alkaline_reserve_pct": 1, "max_color_delta_e": 3, "sample_ratio": 1}}
	first := request(t, server, http.MethodPost, "/api/v1/batches", "operator", create)
	if first.Code != http.StatusCreated {
		t.Fatalf("创建失败: %d %s", first.Code, first.Body.String())
	}
	replay := request(t, server, http.MethodPost, "/api/v1/batches", "operator", create)
	if replay.Code != http.StatusCreated || replay.Header().Get("X-Idempotent-Replay") != "true" || replay.Body.String() != first.Body.String() {
		t.Fatalf("幂等重放未返回原始结果: %d %q", replay.Code, replay.Header().Get("X-Idempotent-Replay"))
	}
	freeze := request(t, server, http.MethodPost, "/api/v1/batches/batch-1/baseline", "operator", map[string]any{"request_id": "freeze-1", "expected_revision": 99})
	if freeze.Code != http.StatusConflict {
		t.Fatalf("过期修订应返回 409: %d %s", freeze.Code, freeze.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(freeze.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "revision_conflict" || body.Error.CurrentRevision != 1 {
		t.Fatalf("修订冲突响应错误: %+v", body.Error)
	}
}

func TestReviewerIndependenceAtHTTPBoundary(t *testing.T) {
	server := testServer(t)
	create := map[string]any{"request_id": "create-1", "expected_revision": 0, "batch_id": "batch-1", "title": "测试", "operator_id": "same-person", "reviewer_id": "same-person", "standards": map[string]any{"target_surface_ph_min": 7, "target_surface_ph_max": 9, "min_alkaline_reserve_pct": 1, "max_color_delta_e": 3, "sample_ratio": 1}}
	resp := request(t, server, http.MethodPost, "/api/v1/batches", "same-person", create)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("角色不独立应返回 403: %d %s", resp.Code, resp.Body.String())
	}
}

func TestUnknownRouteUsesStableJSONError(t *testing.T) {
	server := testServer(t)
	resp := request(t, server, http.MethodGet, "/api/v1/unknown", "", nil)
	if resp.Code != http.StatusNotFound || resp.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("未知路由响应错误: %d %s", resp.Code, resp.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "route_not_found" || body.Error.CurrentRevision != 0 {
		t.Fatalf("未知路由错误体不稳定: %+v", body.Error)
	}
}
