package timeline_resource_error_chain_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/store"
)

func TestTimelineResourceErrorPreservesCause(t *testing.T) {
	root := t.TempDir()
	repo, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(repo)
	_, err = service.Create(
		application.CommandMeta{RequestID: "create-error-chain", ActorID: "operator"},
		application.CreateBatch{
			BatchID:    "batch-error-chain",
			Title:      "事件资源错误链复现",
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

	eventsPath := filepath.Join(root, "events")
	eventPath := filepath.Join(eventsPath, "batch-error-chain.log")
	if err := os.Remove(eventPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(eventsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventsPath, []byte("invalid event resource"), 0o640); err != nil {
		t.Fatal(err)
	}

	_, err = service.QueryTimeline("batch-error-chain", application.TimelineQuery{Limit: 50})
	if err == nil {
		t.Fatal("事件日志路径失效后查询应返回错误")
	}
	if !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("事件资源失效的错误链丢失，无法识别 ENOTDIR: %v", err)
	}
}
