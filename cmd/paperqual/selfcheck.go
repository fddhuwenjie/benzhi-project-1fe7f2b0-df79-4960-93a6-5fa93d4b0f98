package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"paperqual/internal/api"
	"paperqual/internal/application"
	"paperqual/internal/store"
)

type selfCheckClient struct {
	baseURL string
	client  *http.Client
}

func runSelfCheck(address string) error {
	dataDir, err := os.MkdirTemp("", "paperqual-self-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dataDir)
	repo, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	service := application.NewService(repo)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("自检监听 %s 失败: %w", address, err)
	}
	httpServer := &http.Server{Handler: api.NewServer(service), ReadHeaderTimeout: 3 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	client := &selfCheckClient{baseURL: "http://" + listener.Addr().String(), client: &http.Client{Timeout: 5 * time.Second}}
	flowErr := client.runReleaseFlow()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := httpServer.Shutdown(shutdownCtx)
	cancel()
	serveErr := <-done
	if flowErr != nil {
		return flowErr
	}
	if shutdownErr != nil {
		return shutdownErr
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return serveErr
	}
	fmt.Println("自检通过：完整放行流程、时间线与证书摘要链均有效")
	return nil
}

func (c *selfCheckClient) runReleaseFlow() error {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	create := map[string]any{"request_id": "req-create", "expected_revision": 0, "batch_id": "self-check-batch", "title": "自检脱酸处理批次", "operator_id": "operator-1", "reviewer_id": "reviewer-1", "standards": map[string]any{"target_surface_ph_min": 7.0, "target_surface_ph_max": 9.5, "min_alkaline_reserve_pct": 1.5, "max_color_delta_e": 3.0, "sample_ratio": 1.0}}
	if _, err := c.post("/api/v1/batches", "operator-1", create, http.StatusCreated); err != nil {
		return err
	}
	item := map[string]any{"request_id": "req-item", "expected_revision": 1, "item_id": "item-001", "shelf_mark": "A-001", "paper_type": "机制纸", "baseline_surface_ph": 5.1, "baseline_cold_extract_ph": 5.0, "measurement_points": 5, "source_digest": digest}
	if _, err := c.post("/api/v1/batches/self-check-batch/items", "operator-1", item, http.StatusOK); err != nil {
		return err
	}
	if _, err := c.post("/api/v1/batches/self-check-batch/baseline", "operator-1", map[string]any{"request_id": "req-freeze", "expected_revision": 2}, http.StatusOK); err != nil {
		return err
	}
	round := map[string]any{"request_id": "req-round", "expected_revision": 3, "round_id": "round-001", "round_kind": "treatment", "started_at": "2026-01-02T03:00:00Z", "completed_at": "2026-01-02T04:00:00Z", "evidence_digest": digest, "measurements": []map[string]any{{"item_id": "item-001", "surface_ph": 8.1, "cold_extract_ph": 7.8, "alkaline_reserve_pct": 2.2, "color_delta_e": 1.1, "source_digest": digest}}}
	if _, err := c.post("/api/v1/batches/self-check-batch/treatment-rounds", "operator-1", round, http.StatusOK); err != nil {
		return err
	}
	if _, err := c.post("/api/v1/batches/self-check-batch/review", "reviewer-1", map[string]any{"request_id": "req-review", "expected_revision": 4, "review_id": "review-001"}, http.StatusCreated); err != nil {
		return err
	}
	review := map[string]any{"request_id": "req-sign", "expected_revision": 5, "item_decisions": []map[string]any{{"item_id": "item-001", "decision": "accept"}}}
	if _, err := c.post("/api/v1/batches/self-check-batch/review/decision", "reviewer-1", review, http.StatusOK); err != nil {
		return err
	}
	decision := map[string]any{"request_id": "req-decision", "expected_revision": 6, "decision": "release"}
	if _, err := c.post("/api/v1/batches/self-check-batch/decision", "reviewer-1", decision, http.StatusOK); err != nil {
		return err
	}
	var verification struct {
		Valid bool `json:"valid"`
	}
	if err := c.get("/api/v1/batches/self-check-batch/certificate/verify", &verification); err != nil {
		return err
	}
	if !verification.Valid {
		return fmt.Errorf("自检证书完整性结果为无效")
	}
	var timeline struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := c.get("/api/v1/batches/self-check-batch/timeline", &timeline); err != nil {
		return err
	}
	if len(timeline.Events) != 7 {
		return fmt.Errorf("自检时间线事件数异常：%d", len(timeline.Events))
	}
	return nil
}

func (c *selfCheckClient) post(path, actor string, body any, expected int) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Actor-ID", actor)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != expected {
		return nil, fmt.Errorf("POST %s 返回 %d，期望 %d：%s", path, resp.StatusCode, expected, string(response))
	}
	return response, nil
}

func (c *selfCheckClient) get(path string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		response, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return fmt.Errorf("GET %s 返回 %d：%s", path, resp.StatusCode, string(response))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
