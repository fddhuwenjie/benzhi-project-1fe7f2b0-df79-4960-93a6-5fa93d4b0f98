package restart_corrupt_tail_truncation_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"paperqual/internal/domain"
	"paperqual/internal/store"
)

func TestRestartDoesNotTruncateCompleteCorruptTail(t *testing.T) {
	mutations := map[string]func(t *testing.T, frame map[string]any){
		"payload-digest": func(t *testing.T, frame map[string]any) {
			frame["payload_digest"] = differentDigest(t, frame["payload_digest"])
		},
		"frame-digest": func(t *testing.T, frame map[string]any) {
			frame["frame_digest"] = differentDigest(t, frame["frame_digest"])
		},
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := createCommittedBatch(t, root)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取事件日志: %v", err)
			}
			corrupted := mutateOnlyFrame(t, original, mutate)
			if err := os.WriteFile(path, corrupted, 0o640); err != nil {
				t.Fatalf("写入完整损坏帧: %v", err)
			}

			if _, err := store.Open(root); err == nil {
				t.Fatal("完整但损坏的已提交尾帧应使恢复失败")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取恢复后的事件日志: %v", err)
			}
			if !bytes.Equal(after, corrupted) {
				t.Fatalf("失败的重启恢复截断了完整已提交尾帧: before=%d after=%d", len(corrupted), len(after))
			}
		})
	}
}

func createCommittedBatch(t *testing.T, root string) string {
	t.Helper()
	repo, err := store.Open(root)
	if err != nil {
		t.Fatalf("打开仓储: %v", err)
	}
	batch, err := domain.NewBatch("corrupt-tail", "尾帧恢复", "operator", "reviewer", domain.Standards{
		TargetSurfacePHMin: 7, TargetSurfacePHMax: 9, MinAlkalineReservePct: 1,
		MaxColorDeltaE: 3, SampleRatio: 1,
	}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("创建领域批次: %v", err)
	}
	_, err = repo.Create(store.CommitRequest{
		Batch: *batch,
		Event: store.EventPayload{
			EventType: "batch.created", ActorID: "operator", RequestID: "create",
			Revision: 1, OccurredAt: time.Unix(10, 0), Data: json.RawMessage(`{"batch_id":"corrupt-tail"}`),
		},
		RequestID: "create", Fingerprint: domain.Digest([]byte("create")),
		StatusCode: 201, Response: json.RawMessage(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("持久化批次: %v", err)
	}
	return filepath.Join(root, "events", "corrupt-tail.log")
}

func mutateOnlyFrame(t *testing.T, record []byte, mutate func(*testing.T, map[string]any)) []byte {
	t.Helper()
	if len(record) < 10 || record[len(record)-1] != '\n' {
		t.Fatalf("事件记录边界无效: %q", record)
	}
	var frame map[string]any
	if err := json.Unmarshal(record[8:len(record)-1], &frame); err != nil {
		t.Fatalf("解析原事件帧: %v", err)
	}
	mutate(t, frame)
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("编码损坏事件帧: %v", err)
	}
	return []byte(fmt.Sprintf("%08x%s\n", len(body), body))
}

func differentDigest(t *testing.T, value any) string {
	t.Helper()
	digest, ok := value.(string)
	if !ok || len(digest) != 64 {
		t.Fatalf("原摘要无效: %#v", value)
	}
	if digest[0] == '0' {
		return "1" + digest[1:]
	}
	return "0" + digest[1:]
}
