package snapshot_read_cancel_stall_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"paperqual/internal/application"
	"paperqual/internal/store"
)

func TestCanceledSnapshotReadStopsTreatmentSubmission(t *testing.T) {
	root := t.TempDir()
	repo, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}

	const batchID = "blocked-batch"
	snapshotPath := filepath.Join(root, "snapshots", batchID+".json")
	if err := syscall.Mkfifo(snapshotPath, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	service := application.NewService(repo)
	go func() {
		_, callErr := service.SubmitTreatmentRoundContext(ctx, batchID, application.CommandMeta{
			RequestID:        "round-cancel",
			ExpectedRevision: 1,
			ActorID:          "operator",
		}, application.SubmitRound{RoundID: "round-1", RoundKind: "treatment"})
		result <- callErr
	}()

	// FIFO 的写端成功打开，确定性证明仓储已经打开读端并阻塞在快照读取中。
	writer, err := os.OpenFile(snapshotPath, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	cancel()
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case callErr := <-result:
		if !errors.Is(callErr, context.Canceled) {
			t.Fatalf("TestCanceledSnapshotReadStopsTreatmentSubmission: 期望 context.Canceled，实际为 %v", callErr)
		}
	case <-timer.C:
		t.Fatal("TestCanceledSnapshotReadStopsTreatmentSubmission: context 取消后快照读取仍阻塞")
	}
}
