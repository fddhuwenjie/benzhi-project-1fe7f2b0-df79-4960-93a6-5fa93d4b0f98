package snapshot_cache_rollback_alias_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/store"
)

func TestFailedCommitDoesNotPolluteCachedBatch(t *testing.T) {
	root := t.TempDir()
	repo, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	snapshotDir := filepath.Join(root, "snapshots")
	backupDir := filepath.Join(root, "snapshots-backup")
	armed := false
	sabotaged := false
	service := application.NewServiceWithClock(repo, func() time.Time {
		if armed && !sabotaged {
			if err := os.Rename(snapshotDir, backupDir); err != nil {
				t.Fatalf("失效快照目录: %v", err)
			}
			if err := os.WriteFile(snapshotDir, []byte("阻断原子快照写入"), 0o640); err != nil {
				t.Fatalf("建立失效快照路径: %v", err)
			}
			sabotaged = true
		}
		return now
	})
	digest := strings.Repeat("a", 64)

	result, err := service.Create(meta("create", 0), application.CreateBatch{
		BatchID: "batch-cache", Title: "缓存回滚复现", OperatorID: "operator", ReviewerID: "reviewer",
		Standards: domain.Standards{TargetSurfacePHMin: 7, TargetSurfacePHMax: 9, MinAlkalineReservePct: 1, MaxColorDeltaE: 3, SampleRatio: 1},
	})
	mustSucceed(t, result, err)
	result, err = service.RegisterItem("batch-cache", meta("item", 1), application.RegisterItem{
		ItemID: "item-1", ShelfMark: "A-1", PaperType: "宣纸", BaselineSurfacePH: 6.5,
		BaselineColdExtractPH: 6.4, MeasurementPoints: 3, SourceDigest: digest,
	})
	mustSucceed(t, result, err)
	result, err = service.FreezeBaseline("batch-cache", meta("freeze", 2))
	mustSucceed(t, result, err)

	before, err := service.GetBatch("batch-cache")
	if err != nil {
		t.Fatal(err)
	}
	if before.Batch.Items[0].QualificationStatus != "pending" {
		t.Fatalf("初始档案件状态异常: %s", before.Batch.Items[0].QualificationStatus)
	}

	restored := false
	defer func() {
		if sabotaged && !restored {
			_ = os.Remove(snapshotDir)
			_ = os.Rename(backupDir, snapshotDir)
		}
	}()

	armed = true
	_, err = service.SubmitTreatmentRound("batch-cache", meta("round", 3), application.SubmitRound{
		RoundID: "round-1", RoundKind: "treatment", StartedAt: now.Add(-2 * time.Minute), CompletedAt: now.Add(-time.Minute),
		Measurements:   []domain.Measurement{{ItemID: "item-1", SurfacePH: 7.5, ColdExtractPH: 7.4, AlkalineReservePct: 2, ColorDeltaE: 1, SourceDigest: digest}},
		EvidenceDigest: digest,
	})
	if err == nil {
		t.Fatal("快照路径失效时处理轮次应提交失败")
	}
	if !sabotaged {
		t.Fatal("未在领域转换后触发快照路径失效")
	}
	if err := os.Remove(snapshotDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backupDir, snapshotDir); err != nil {
		t.Fatal(err)
	}
	restored = true

	after, err := service.GetBatch("batch-cache")
	if err != nil {
		t.Fatal(err)
	}
	item := after.Batch.Items[0]
	if item.QualificationStatus != "pending" || item.LatestRoundID != "" || len(item.FailureCodes) != 0 {
		t.Fatalf("失败提交污染了缓存批次: qualification_status=%q latest_round_id=%q failure_codes=%v", item.QualificationStatus, item.LatestRoundID, item.FailureCodes)
	}
}

func meta(requestID string, revision int64) application.CommandMeta {
	return application.CommandMeta{RequestID: requestID, ExpectedRevision: revision, ActorID: "operator"}
}

func mustSucceed(t *testing.T, _ application.Result, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
