package store

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"paperqual/internal/domain"
	"paperqual/internal/evidence"
)

func (r *Repository) writeSnapshot(batchID string, snap Snapshot) error {
	raw, err := evidence.CanonicalJSON(snap)
	if err != nil {
		return err
	}
	dir := filepath.Dir(r.snapshotPath(batchID))
	tmp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o640); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, r.snapshotPath(batchID)); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	ok = true
	return nil
}

func (r *Repository) appendFrame(batchID string, frame EventFrame) error {
	raw, err := evidence.CanonicalJSON(frame)
	if err != nil {
		return err
	}
	if len(raw) > 16*1024*1024 {
		return fmt.Errorf("事件帧过大")
	}
	header := fmt.Sprintf("%08x", len(raw))
	record := make([]byte, 0, 8+len(raw)+1)
	record = append(record, header...)
	record = append(record, raw...)
	record = append(record, '\n')
	f, err := os.OpenFile(r.eventPath(batchID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(record); err != nil {
		return err
	}
	return f.Sync()
}

func (r *Repository) readTimeline(batchID string, recoverTail bool) ([]TimelineEntry, string, error) {
	path := r.eventPath(batchID)
	f, err := os.OpenFile(path, os.O_RDONLY, 0o640)
	if os.IsNotExist(err) {
		return []TimelineEntry{}, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	entries := []TimelineEntry{}
	anchor := ""
	var offset int64
	for {
		start := offset
		header, err := readExact(reader, 8)
		if err == io.EOF {
			return entries, anchor, nil
		}
		if err != nil {
			if recoverTail {
				return r.truncateTail(path, start, entries, anchor)
			}
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件帧头截断")
		}
		offset += 8
		if _, err := hex.DecodeString(string(header)); err != nil {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件帧长度头无效")
		}
		size64, err := strconv.ParseUint(string(header), 16, 32)
		if err != nil || size64 == 0 || size64 > 16*1024*1024 {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件帧长度无效")
		}
		body, err := readExact(reader, int(size64)+1)
		if err != nil {
			if recoverTail {
				return r.truncateTail(path, start, entries, anchor)
			}
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件尾帧截断")
		}
		offset += int64(len(body))
		if body[len(body)-1] != '\n' {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件帧缺少换行边界")
		}
		var frame EventFrame
		if err := json.Unmarshal(body[:len(body)-1], &frame); err != nil {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件帧 JSON 无效")
		}
		if frame.Sequence != uint64(len(entries)+1) || frame.PreviousDigest != anchor {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件序号或前序摘要不连续")
		}
		if domain.Digest(frame.Payload) != frame.PayloadDigest {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件载荷摘要不匹配")
		}
		unsigned := struct {
			Sequence       uint64          `json:"sequence"`
			PreviousDigest string          `json:"previous_digest"`
			PayloadDigest  string          `json:"payload_digest"`
			Payload        json.RawMessage `json:"payload"`
		}{frame.Sequence, frame.PreviousDigest, frame.PayloadDigest, frame.Payload}
		digest, _, err := evidence.Digest(unsigned)
		if err != nil {
			return nil, "", err
		}
		if digest != frame.FrameDigest {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件帧摘要不匹配")
		}
		var payload EventPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return nil, "", domain.Errorf(domain.CodeEvidenceCorrupt, "事件载荷 JSON 无效")
		}
		entries = append(entries, TimelineEntry{Sequence: frame.Sequence, Digest: frame.FrameDigest, Event: payload})
		anchor = frame.FrameDigest
	}
}

func readExact(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	read, err := io.ReadFull(r, buf)
	if err == io.EOF && read == 0 {
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (r *Repository) truncateTail(path string, size int64, entries []TimelineEntry, anchor string) ([]TimelineEntry, string, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0o640)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		return nil, "", err
	}
	if err := f.Sync(); err != nil {
		return nil, "", err
	}
	return entries, anchor, nil
}

func (r *Repository) truncateEventsAfter(batchID string, sequence uint64) error {
	path := r.eventPath(batchID)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	var offset int64
	for current := uint64(0); current < sequence; current++ {
		header, err := readExact(reader, 8)
		if err != nil {
			f.Close()
			return domain.Errorf(domain.CodeEvidenceCorrupt, "恢复时事件帧头截断")
		}
		size, err := strconv.ParseUint(string(header), 16, 32)
		if err != nil || size == 0 || size > 16*1024*1024 {
			f.Close()
			return domain.Errorf(domain.CodeEvidenceCorrupt, "恢复时事件帧长度无效")
		}
		if _, err := readExact(reader, int(size)+1); err != nil {
			f.Close()
			return domain.Errorf(domain.CodeEvidenceCorrupt, "恢复时事件帧截断")
		}
		offset += int64(8 + size + 1)
	}
	if err := f.Close(); err != nil {
		return err
	}
	writable, err := os.OpenFile(path, os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer writable.Close()
	if err := writable.Truncate(offset); err != nil {
		return err
	}
	return writable.Sync()
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
