package store

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"paperqual/internal/domain"
	"paperqual/internal/evidence"
)

func createStoredBatch(t *testing.T, root string) (*Repository, Snapshot) {
	t.Helper()
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := domain.NewBatch("batch-1", "测试", "operator", "reviewer", domain.Standards{TargetSurfacePHMin: 7, TargetSurfacePHMax: 9, MinAlkalineReservePct: 1, MaxColorDeltaE: 3, SampleRatio: 1}, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	event := EventPayload{EventType: "batch.created", ActorID: "operator", RequestID: "request-1", Revision: 1, OccurredAt: time.Unix(10, 0), Data: json.RawMessage(`{"batch_id":"batch-1"}`)}
	snap, err := repo.Create(CommitRequest{Batch: *batch, Event: event, RequestID: "request-1", Fingerprint: domain.Digest([]byte("request-1")), StatusCode: 201, Response: json.RawMessage(`{"ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	return repo, snap
}

func TestPersistentIdempotencyAndRevisionCheck(t *testing.T) {
	repo, snap := createStoredBatch(t, t.TempDir())
	entry, ok, err := repo.Lookup("batch-1", "request-1", domain.Digest([]byte("request-1")))
	if err != nil || !ok || entry.StatusCode != 201 {
		t.Fatalf("幂等索引读取失败: ok=%v entry=%+v err=%v", ok, entry, err)
	}
	if _, _, err := repo.Lookup("batch-1", "request-1", domain.Digest([]byte("different"))); err == nil {
		t.Fatal("不同指纹重用 request_id 应冲突")
	}
	snap.Batch.Revision = 2
	_, err = repo.Commit(CommitRequest{Batch: snap.Batch, ExpectedBase: 99, Event: EventPayload{}, RequestID: "request-2", Fingerprint: domain.Digest([]byte("request-2")), Response: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("陈旧 expected revision 应冲突")
	}
}

func TestOpenRecoversIncompleteAndUncommittedTail(t *testing.T) {
	root := t.TempDir()
	repo, snap := createStoredBatch(t, root)
	path := repo.eventPath("batch-1")
	committedInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("00000100{\"partial\":")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := Open(root); err != nil {
		t.Fatalf("不完整尾帧应恢复: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Size() != committedInfo.Size() {
		t.Fatalf("不完整尾帧未截断: %d != %d", info.Size(), committedInfo.Size())
	}
	payload := EventPayload{EventType: "orphan", ActorID: "operator", RequestID: "orphan", Revision: 2, OccurredAt: time.Unix(20, 0)}
	payloadRaw, _ := evidence.CanonicalJSON(payload)
	frame := EventFrame{Sequence: 2, PreviousDigest: snap.EventAnchor, PayloadDigest: domain.Digest(payloadRaw), Payload: payloadRaw}
	unsigned := struct {
		Sequence       uint64          `json:"sequence"`
		PreviousDigest string          `json:"previous_digest"`
		PayloadDigest  string          `json:"payload_digest"`
		Payload        json.RawMessage `json:"payload"`
	}{frame.Sequence, frame.PreviousDigest, frame.PayloadDigest, frame.Payload}
	frame.FrameDigest, _, _ = evidence.Digest(unsigned)
	if err := repo.appendFrame("batch-1", frame); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err != nil {
		t.Fatalf("快照之后的完整孤立事件应回退: %v", err)
	}
	entries, anchor, err := repo.Timeline("batch-1")
	if err != nil || len(entries) != 1 || anchor != snap.EventAnchor {
		t.Fatalf("恢复后事件链错误: len=%d anchor=%s err=%v", len(entries), anchor, err)
	}
}

func TestOpenRejectsMiddleCorruption(t *testing.T) {
	root := t.TempDir()
	repo, _ := createStoredBatch(t, root)
	path := repo.eventPath("batch-1")
	f, err := os.OpenFile(path, os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("zzzzzzzz"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := Open(root); err == nil {
		t.Fatal("中段帧头损坏应拒绝打开")
	}
}
