package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"paperqual/internal/domain"
)

func TestTimelineQueryPaginatesVerifiedSnapshotAndDetectsChange(t *testing.T) {
	repo, snap := createStoredBatch(t, t.TempDir())
	for i, eventType := range []string{"items.batch_registered", "baseline.frozen", "round.submitted"} {
		snap.Batch.Revision++
		requestID := "request-more-" + string(rune('1'+i))
		var err error
		snap, err = repo.Commit(CommitRequest{Batch: snap.Batch, ExpectedBase: snap.Batch.Revision - 1, Event: EventPayload{EventType: eventType, ActorID: "operator", RequestID: requestID, Revision: snap.Batch.Revision, OccurredAt: time.Unix(int64(20+i), 0)}, RequestID: requestID, Fingerprint: domain.Digest([]byte(requestID)), StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.QueryTimeline("batch-1", TimelineQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalEvents != 4 || first.FirstSequence != 1 || first.LastSequence != 2 || first.NextCursor == nil {
		t.Fatalf("首屏元数据不正确: %+v", first)
	}
	second, err := repo.QueryTimeline("batch-1", TimelineQuery{Limit: 2, Cursor: first.NextCursor, SnapshotAnchor: first.EventAnchor})
	if err != nil {
		t.Fatal(err)
	}
	if second.EventAnchor != first.EventAnchor || second.FirstSequence != 3 || second.LastSequence != 4 || second.NextCursor != nil {
		t.Fatalf("第二页不连续: %+v", second)
	}

	oldAnchor := first.EventAnchor
	snap.Batch.Revision++
	requestID := "request-review"
	if _, err := repo.Commit(CommitRequest{Batch: snap.Batch, ExpectedBase: snap.Batch.Revision - 1, Event: EventPayload{EventType: "review.started", ActorID: "reviewer", RequestID: requestID, Revision: snap.Batch.Revision, OccurredAt: time.Unix(30, 0)}, RequestID: requestID, Fingerprint: domain.Digest([]byte(requestID)), StatusCode: 201, Response: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	_, err = repo.QueryTimeline("batch-1", TimelineQuery{Limit: 2, Cursor: first.NextCursor, SnapshotAnchor: oldAnchor})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeTimelineChanged {
		t.Fatalf("旧锚点应返回 timeline_changed: %#v", err)
	}
}

func TestTimelineQueryFiltersAfterFullChainVerification(t *testing.T) {
	repo, snap := createStoredBatch(t, t.TempDir())
	snap.Batch.Revision = 2
	if _, err := repo.Commit(CommitRequest{Batch: snap.Batch, ExpectedBase: 1, Event: EventPayload{EventType: "review.started", ActorID: "reviewer", RequestID: "request-2", Revision: 2, OccurredAt: time.Unix(20, 0)}, RequestID: "request-2", Fingerprint: domain.Digest([]byte("request-2")), StatusCode: 201, Response: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	min := int64(2)
	page, err := repo.QueryTimeline("batch-1", TimelineQuery{Limit: 10, ActorID: "reviewer", MinRevision: &min})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalEvents != 2 || page.FilteredEvents != 1 || len(page.Events) != 1 || page.Events[0].Sequence != 2 {
		t.Fatalf("筛选结果或完整链计数错误: %+v", page)
	}
}
