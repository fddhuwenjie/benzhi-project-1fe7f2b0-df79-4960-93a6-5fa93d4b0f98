package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"paperqual/internal/domain"
	"paperqual/internal/evidence"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Repository struct {
	root         string
	locks        [128]sync.RWMutex
	eventFilesMu sync.Mutex
	eventFiles   map[string]*os.File
}

func Open(root string) (*Repository, error) {
	if root == "" {
		return nil, fmt.Errorf("数据目录不能为空")
	}
	for _, dir := range []string{root, filepath.Join(root, "snapshots"), filepath.Join(root, "events")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}
	r := &Repository{root: root, eventFiles: map[string]*os.File{}}
	batchIDs := map[string]bool{}
	locations := []struct {
		directory string
		extension string
	}{{filepath.Join(root, "events"), ".log"}, {filepath.Join(root, "snapshots"), ".json"}}
	for _, location := range locations {
		entries, err := os.ReadDir(location.directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != location.extension {
				continue
			}
			id := entry.Name()[:len(entry.Name())-len(location.extension)]
			if !safeID.MatchString(id) {
				return nil, fmt.Errorf("数据目录包含非法批次文件名 %s", entry.Name())
			}
			batchIDs[id] = true
		}
	}
	for id := range batchIDs {
		timeline, _, err := r.readTimeline(id, true)
		if err != nil {
			return nil, fmt.Errorf("验证批次 %s 事件链: %w", id, err)
		}
		if err := r.reconcileSnapshot(id, timeline); err != nil {
			return nil, fmt.Errorf("恢复批次 %s 提交边界: %w", id, err)
		}
	}
	return r, nil
}

func (r *Repository) reconcileSnapshot(batchID string, timeline []TimelineEntry) error {
	raw, err := os.ReadFile(r.snapshotPath(batchID))
	if errors.Is(err, fs.ErrNotExist) {
		if len(timeline) == 0 {
			return nil
		}
		return r.truncateEventsAfter(batchID, 0)
	}
	if err != nil {
		return err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return domain.Errorf(domain.CodeEvidenceCorrupt, "快照 JSON 损坏")
	}
	if snap.Sequence > uint64(len(timeline)) {
		return domain.Errorf(domain.CodeEvidenceCorrupt, "快照序号超出事件链")
	}
	if snap.Sequence == 0 {
		if snap.EventAnchor != "" {
			return domain.Errorf(domain.CodeEvidenceCorrupt, "空事件链快照锚点无效")
		}
	} else if timeline[snap.Sequence-1].Digest != snap.EventAnchor {
		return domain.Errorf(domain.CodeEvidenceCorrupt, "快照锚点与已提交事件不一致")
	}
	if snap.Sequence < uint64(len(timeline)) {
		return r.truncateEventsAfter(batchID, snap.Sequence)
	}
	return nil
}

func (r *Repository) Load(batchID string) (Snapshot, error) {
	if !safeID.MatchString(batchID) {
		return Snapshot{}, domain.Errorf(domain.CodeValidation, "batch_id 格式无效")
	}
	lock := r.lockFor(batchID)
	lock.RLock()
	defer lock.RUnlock()
	return r.loadUnlocked(batchID)
}

func (r *Repository) loadUnlocked(batchID string) (Snapshot, error) {
	raw, err := os.ReadFile(r.snapshotPath(batchID))
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, domain.Errorf(domain.CodeNotFound, "批次 %s 不存在", batchID)
	}
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, domain.Errorf(domain.CodeEvidenceCorrupt, "快照 JSON 损坏")
	}
	if snap.Idempotency == nil {
		snap.Idempotency = map[string]IdempotencyEntry{}
	}
	entries, anchor, err := r.readTimeline(batchID, true)
	if err != nil {
		return Snapshot{}, err
	}
	if uint64(len(entries)) != snap.Sequence || anchor != snap.EventAnchor {
		return Snapshot{}, domain.Errorf(domain.CodeEvidenceCorrupt, "快照锚点与事件链不一致")
	}
	return snap, nil
}

func (r *Repository) Lookup(batchID, requestID, fingerprint string) (IdempotencyEntry, bool, error) {
	snap, err := r.Load(batchID)
	if err != nil {
		return IdempotencyEntry{}, false, err
	}
	entry, ok := snap.Idempotency[requestID]
	if !ok {
		return IdempotencyEntry{}, false, nil
	}
	if entry.Fingerprint != fingerprint {
		return IdempotencyEntry{}, false, domain.WithRevision(domain.Errorf(domain.CodeIdempotency, "request_id 已被不同请求使用"), snap.Batch.Revision)
	}
	return entry, true, nil
}

func (r *Repository) Create(req CommitRequest) (Snapshot, error) {
	if !safeID.MatchString(req.Batch.BatchID) {
		return Snapshot{}, domain.Errorf(domain.CodeValidation, "batch_id 格式无效")
	}
	lock := r.lockFor(req.Batch.BatchID)
	lock.Lock()
	defer lock.Unlock()
	if _, err := os.Stat(r.snapshotPath(req.Batch.BatchID)); err == nil {
		return Snapshot{}, domain.Errorf(domain.CodeValidation, "batch_id 已存在")
	}
	snap := Snapshot{Batch: req.Batch, Idempotency: map[string]IdempotencyEntry{}}
	return r.commitUnlocked(snap, req, true)
}

func (r *Repository) Commit(req CommitRequest) (Snapshot, error) {
	lock := r.lockFor(req.Batch.BatchID)
	lock.Lock()
	defer lock.Unlock()
	snap, err := r.loadUnlocked(req.Batch.BatchID)
	if err != nil {
		return Snapshot{}, err
	}
	if entry, ok := snap.Idempotency[req.RequestID]; ok {
		if entry.Fingerprint != req.Fingerprint {
			return Snapshot{}, domain.WithRevision(domain.Errorf(domain.CodeIdempotency, "request_id 已被不同请求使用"), snap.Batch.Revision)
		}
		return snap, nil
	}
	if snap.Batch.Revision != req.ExpectedBase {
		return Snapshot{}, domain.WithRevision(domain.Errorf(domain.CodeRevisionConflict, "expected_revision 已过期"), snap.Batch.Revision)
	}
	return r.commitUnlocked(snap, req, false)
}

func (r *Repository) commitUnlocked(snap Snapshot, req CommitRequest, create bool) (Snapshot, error) {
	if req.RequestID == "" || req.Fingerprint == "" {
		return Snapshot{}, domain.Errorf(domain.CodeValidation, "request_id 和请求指纹不能为空")
	}
	if create && snap.Sequence != 0 {
		return Snapshot{}, fmt.Errorf("创建提交序号无效")
	}
	payload, err := evidence.CanonicalJSON(req.Event)
	if err != nil {
		return Snapshot{}, err
	}
	payloadDigest := domain.Digest(payload)
	frame := EventFrame{Sequence: snap.Sequence + 1, PreviousDigest: snap.EventAnchor, PayloadDigest: payloadDigest, Payload: payload}
	unsigned := struct {
		Sequence       uint64          `json:"sequence"`
		PreviousDigest string          `json:"previous_digest"`
		PayloadDigest  string          `json:"payload_digest"`
		Payload        json.RawMessage `json:"payload"`
	}{frame.Sequence, frame.PreviousDigest, frame.PayloadDigest, frame.Payload}
	frameDigest, _, err := evidence.Digest(unsigned)
	if err != nil {
		return Snapshot{}, err
	}
	frame.FrameDigest = frameDigest
	if err := r.appendFrame(req.Batch.BatchID, frame); err != nil {
		return Snapshot{}, err
	}
	snap.Batch = req.Batch
	snap.Sequence = frame.Sequence
	snap.EventAnchor = frameDigest
	snap.Idempotency[req.RequestID] = IdempotencyEntry{Fingerprint: req.Fingerprint, StatusCode: req.StatusCode, Response: append(json.RawMessage(nil), req.Response...), Revision: req.Batch.Revision, CreatedAt: req.Event.OccurredAt.UTC()}
	if len(req.Certificate) > 0 {
		snap.Certificate = append(json.RawMessage(nil), req.Certificate...)
	}
	if err := r.writeSnapshot(req.Batch.BatchID, snap); err != nil {
		rollbackErr := r.truncateEventsAfter(req.Batch.BatchID, frame.Sequence-1)
		if rollbackErr != nil {
			return Snapshot{}, fmt.Errorf("写入快照失败且事件回退失败: %v; %w", rollbackErr, err)
		}
		return Snapshot{}, err
	}
	return snap, nil
}

func (r *Repository) Timeline(batchID string) ([]TimelineEntry, string, error) {
	lock := r.lockFor(batchID)
	lock.RLock()
	defer lock.RUnlock()
	return r.readTimeline(batchID, false)
}

func (r *Repository) QueryTimeline(batchID string, query TimelineQuery) (TimelinePage, error) {
	if query.Limit < 1 || query.Limit > 100 {
		return TimelinePage{}, domain.Errorf(domain.CodeInvalidQuery, "limit 必须在 1 到 100 之间")
	}
	if query.MinRevision != nil && *query.MinRevision < 1 {
		return TimelinePage{}, domain.Errorf(domain.CodeInvalidQuery, "min_revision 必须大于 0")
	}
	if query.MaxRevision != nil && *query.MaxRevision < 1 {
		return TimelinePage{}, domain.Errorf(domain.CodeInvalidQuery, "max_revision 必须大于 0")
	}
	if query.MinRevision != nil && query.MaxRevision != nil && *query.MinRevision > *query.MaxRevision {
		return TimelinePage{}, domain.Errorf(domain.CodeInvalidQuery, "min_revision 不能大于 max_revision")
	}
	lock := r.lockFor(batchID)
	lock.RLock()
	defer lock.RUnlock()
	snap, err := r.loadUnlocked(batchID)
	if err != nil {
		return TimelinePage{}, err
	}
	if query.SnapshotAnchor != "" && query.SnapshotAnchor != snap.EventAnchor {
		return TimelinePage{}, domain.WithRevision(domain.Errorf(domain.CodeTimelineChanged, "时间线锚点已变化，请重新开始查询"), snap.Batch.Revision)
	}
	entries, anchor, err := r.readTimeline(batchID, false)
	if err != nil {
		return TimelinePage{}, err
	}
	if anchor != snap.EventAnchor || uint64(len(entries)) != snap.Sequence {
		return TimelinePage{}, domain.WithRevision(domain.Errorf(domain.CodeEvidenceCorrupt, "快照锚点与事件链不一致"), snap.Batch.Revision)
	}
	start := uint64(0)
	if query.Cursor != nil {
		start = *query.Cursor
		if start > uint64(len(entries)) {
			return TimelinePage{}, domain.WithRevision(domain.Errorf(domain.CodeInvalidQuery, "cursor 指向不存在的事件序号"), snap.Batch.Revision)
		}
	}
	filtered := make([]TimelineEntry, 0)
	var filteredTotal uint64
	for _, entry := range entries {
		if !timelineMatch(entry, query) {
			continue
		}
		filteredTotal++
		if entry.Sequence > start {
			filtered = append(filtered, entry)
		}
	}
	pageEvents := filtered
	var next *uint64
	if len(pageEvents) > query.Limit {
		pageEvents = pageEvents[:query.Limit]
		cursor := pageEvents[len(pageEvents)-1].Sequence
		next = &cursor
	}
	page := TimelinePage{Events: pageEvents, EventAnchor: anchor, TotalEvents: uint64(len(entries)), FilteredEvents: filteredTotal, NextCursor: next, CurrentRevision: snap.Batch.Revision}
	if len(pageEvents) > 0 {
		page.FirstSequence = pageEvents[0].Sequence
		page.LastSequence = pageEvents[len(pageEvents)-1].Sequence
	}
	return page, nil
}

func timelineMatch(entry TimelineEntry, query TimelineQuery) bool {
	if query.EventType != "" && entry.Event.EventType != query.EventType {
		return false
	}
	if query.ActorID != "" && entry.Event.ActorID != query.ActorID {
		return false
	}
	if query.MinRevision != nil && entry.Event.Revision < *query.MinRevision {
		return false
	}
	if query.MaxRevision != nil && entry.Event.Revision > *query.MaxRevision {
		return false
	}
	return true
}
func (r *Repository) Certificate(batchID string) (json.RawMessage, error) {
	snap, err := r.Load(batchID)
	if err != nil {
		return nil, err
	}
	if len(snap.Certificate) == 0 {
		return nil, domain.WithRevision(domain.Errorf(domain.CodeInvalidState, "批次尚未封存证书"), snap.Batch.Revision)
	}
	return append(json.RawMessage(nil), snap.Certificate...), nil
}
func (r *Repository) Root() string { return r.root }
func (r *Repository) snapshotPath(id string) string {
	return filepath.Join(r.root, "snapshots", id+".json")
}
func (r *Repository) eventPath(id string) string { return filepath.Join(r.root, "events", id+".log") }

func (r *Repository) lockFor(batchID string) *sync.RWMutex {
	var hash uint32 = 2166136261
	for i := 0; i < len(batchID); i++ {
		hash ^= uint32(batchID[i])
		hash *= 16777619
	}
	return &r.locks[int(hash)%len(r.locks)]
}
