package event_log_rotation_stale_fd_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/store"
)

const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRotatedEventLogKeepsCommitReadable(t *testing.T) {
	root := t.TempDir()
	repo, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Unix(100, 0).UTC() }
	service := application.NewServiceWithClock(repo, clock)
	standards := domain.Standards{
		TargetSurfacePHMin:    7,
		TargetSurfacePHMax:    9,
		MinAlkalineReservePct: 1,
		MaxColorDeltaE:        3,
		SampleRatio:           1,
	}
	_, err = service.Create(
		application.CommandMeta{RequestID: "create", ActorID: "operator"},
		application.CreateBatch{BatchID: "batch-rotation", Title: "日志轮转复现", OperatorID: "operator", ReviewerID: "reviewer", Standards: standards},
	)
	if err != nil {
		t.Fatalf("创建批次: %v", err)
	}

	eventPath := filepath.Join(root, "events", "batch-rotation.log")
	rotatedPath := filepath.Join(root, "events", "batch-rotation.log.rotated")
	if err := os.Rename(eventPath, rotatedPath); err != nil {
		t.Fatalf("轮转事件日志: %v", err)
	}
	committedPrefix, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("读取已轮转日志: %v", err)
	}
	if err := os.WriteFile(eventPath, committedPrefix, 0o640); err != nil {
		t.Fatalf("安装同内容新日志: %v", err)
	}

	_, err = service.RegisterItem(
		"batch-rotation",
		application.CommandMeta{RequestID: "item", ExpectedRevision: 1, ActorID: "operator"},
		application.RegisterItem{
			ItemID:                "item-1",
			ShelfMark:             "A-1",
			PaperType:             "机制纸",
			BaselineSurfacePH:     5,
			BaselineColdExtractPH: 5,
			MeasurementPoints:     4,
			SourceDigest:          digest,
		},
	)
	if err != nil {
		t.Fatalf("轮转后的写命令不应失败: %v", err)
	}
	view, err := service.GetBatch("batch-rotation")
	if err != nil {
		t.Fatalf("提交返回成功后批次必须仍可读取: %v", err)
	}
	if view.Batch.Revision != 2 || view.EventSequence != 2 {
		t.Fatalf("提交后状态不完整: revision=%d sequence=%d", view.Batch.Revision, view.EventSequence)
	}
}
