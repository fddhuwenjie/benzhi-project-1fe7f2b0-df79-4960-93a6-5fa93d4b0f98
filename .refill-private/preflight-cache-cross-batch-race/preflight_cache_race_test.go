package preflight_cache_cross_batch_race_test

import (
	"bytes"
	"fmt"
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

func TestConcurrentPreflightCacheIsRaceFree(t *testing.T) {
	repo, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithClock(repo, func() time.Time {
		return time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	})
	prepareFrozenBatch(t, service, "race-a", "item-a")
	prepareFrozenBatch(t, service, "race-b", "item-b")
	server := api.NewServer(service)

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	for _, target := range []struct {
		batchID string
		itemID  string
	}{{"race-a", "item-a"}, {"race-b", "item-b"}} {
		workers.Add(1)
		go func(batchID, itemID string) {
			defer workers.Done()
			<-start
			body := fmt.Sprintf(`{"request_id":"preflight-%s","expected_revision":3,"round_id":"round-%s","round_kind":"treatment","started_at":"2026-08-27T10:00:00Z","completed_at":"2026-08-27T11:00:00Z","measurements":[{"item_id":"%s","surface_ph":8.0,"cold_extract_ph":7.8,"alkaline_reserve_pct":2.0,"color_delta_e":1.0,"source_digest":"%s"}],"evidence_digest":"%s"}`, batchID, batchID, itemID, digest, digest)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/batches/"+batchID+"/treatment-rounds/preflight", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("X-Actor-ID", "operator-1")
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)
			statuses <- recorder.Code
		}(target.batchID, target.itemID)
	}
	close(start)
	workers.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("预检返回状态 %d，期望 %d", status, http.StatusOK)
		}
	}
}

func prepareFrozenBatch(t *testing.T, service *application.Service, batchID, itemID string) {
	t.Helper()
	standards := domain.Standards{TargetSurfacePHMin: 7, TargetSurfacePHMax: 9.5, MinAlkalineReservePct: 1.5, MaxColorDeltaE: 3, SampleRatio: 1}
	if _, err := service.Create(application.CommandMeta{RequestID: "create-" + batchID, ActorID: "operator-1"}, application.CreateBatch{BatchID: batchID, Title: "并发预检批次", OperatorID: "operator-1", ReviewerID: "reviewer-1", Standards: standards}); err != nil {
		t.Fatal(err)
	}
	item := application.RegisterItem{ItemID: itemID, ShelfMark: "shelf-" + itemID, PaperType: "机制纸", BaselineSurfacePH: 5.2, BaselineColdExtractPH: 5.1, MeasurementPoints: 5, SourceDigest: digest}
	if _, err := service.RegisterItem(batchID, application.CommandMeta{RequestID: "item-" + batchID, ExpectedRevision: 1, ActorID: "operator-1"}, item); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FreezeBaseline(batchID, application.CommandMeta{RequestID: "freeze-" + batchID, ExpectedRevision: 2, ActorID: "operator-1"}); err != nil {
		t.Fatal(err)
	}
}
