package timeline_query_cancel_gap_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/store"
)

func TestCanceledTimelineQueryStopsBlockedEventRead(t *testing.T) {
	root := t.TempDir()
	repo, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(repo)
	_, err = service.Create(
		application.CommandMeta{RequestID: "create-request", ActorID: "operator"},
		application.CreateBatch{
			BatchID:    "cancel-query-batch",
			Title:      "取消后的时间线查询",
			OperatorID: "operator",
			ReviewerID: "reviewer",
			Standards: domain.Standards{
				TargetSurfacePHMin:    7,
				TargetSurfacePHMax:    9,
				MinAlkalineReservePct: 1,
				MaxColorDeltaE:        3,
				SampleRatio:           1,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	eventPath := filepath.Join(root, "events", "cancel-query-batch.log")
	committedEvents, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(eventPath, eventPath+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(eventPath, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, queryErr := service.QueryTimelineContext(ctx, "cancel-query-batch", application.TimelineQuery{Limit: 50})
		result <- queryErr
	}()

	// 打开写端会与已经进入事件读取的查询握手，不依赖计时等待。
	writer, err := os.OpenFile(eventPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := writer.Write(committedEvents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if queryErr := <-result; !errors.Is(queryErr, context.Canceled) {
		t.Fatalf("事件读取期间取消后，查询仍扫描仓储并返回: %v", queryErr)
	}
}
