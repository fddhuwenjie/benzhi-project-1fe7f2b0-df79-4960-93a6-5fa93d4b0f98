package canceleditemdetachedcommit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"paperqual/internal/api"
	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/store"
)

const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestCanceledItemRequestDoesNotCommit(t *testing.T) {
	repo, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setup := application.NewServiceWithClock(repo, func() time.Time { return time.Unix(10, 0).UTC() })
	_, err = setup.Create(application.CommandMeta{RequestID: "create", ActorID: "operator"}, application.CreateBatch{
		BatchID: "batch-cancel", Title: "取消写入测试", OperatorID: "operator", ReviewerID: "reviewer",
		Standards: domain.Standards{TargetSurfacePHMin: 7, TargetSurfacePHMax: 9, MinAlkalineReservePct: 1, MaxColorDeltaE: 3, SampleRatio: 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	clockEntered := make(chan struct{})
	clockRelease := make(chan struct{})
	var enterOnce sync.Once
	clock := func() time.Time {
		enterOnce.Do(func() { close(clockEntered) })
		<-clockRelease
		return time.Unix(20, 0).UTC()
	}
	server := api.NewServer(application.NewServiceWithClock(repo, clock))

	ctx, cancel := context.WithCancel(context.Background())
	item := map[string]any{
		"request_id": "item-canceled", "expected_revision": 1,
		"item_id": "item-canceled", "shelf_mark": "A-1", "paper_type": "机制纸",
		"baseline_surface_ph": 5.5, "baseline_cold_extract_ph": 5.4,
		"measurement_points": 4, "source_digest": digest,
	}
	recorder := httptest.NewRecorder()
	request := newJSONRequest(t, ctx, http.MethodPost, "/api/v1/batches/batch-cancel/items", "operator", item)
	handlerDone := make(chan struct{})
	go func() {
		server.ServeHTTP(recorder, request)
		close(handlerDone)
	}()

	<-clockEntered
	cancel()
	<-handlerDone
	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("取消请求应先返回 408，实际 %d: %s", recorder.Code, recorder.Body.String())
	}

	close(clockRelease)
	preflight := map[string]any{
		"request_id": "barrier", "expected_revision": 1,
		"round_id": "round-barrier", "round_kind": "treatment",
		"started_at": "2026-01-02T03:00:00Z", "completed_at": "2026-01-02T04:00:00Z",
		"measurements": []any{}, "evidence_digest": digest,
	}
	barrierRecorder := httptest.NewRecorder()
	server.ServeHTTP(barrierRecorder, newJSONRequest(t, context.Background(), http.MethodPost, "/api/v1/batches/batch-cancel/treatment-rounds/preflight", "operator", preflight))

	snapshot, err := repo.Load("batch-cancel")
	if err != nil {
		t.Fatal(err)
	}
	_, requestPersisted := snapshot.Idempotency["item-canceled"]
	if snapshot.Batch.Revision != 1 || len(snapshot.Batch.Items) != 0 || requestPersisted {
		t.Fatalf("已取消的登记仍被后台命令提交: revision=%d items=%d idempotency=%t", snapshot.Batch.Revision, len(snapshot.Batch.Items), requestPersisted)
	}
}

func newJSONRequest(t *testing.T, ctx context.Context, method, path, actor string, body any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Actor-ID", actor)
	return request
}
