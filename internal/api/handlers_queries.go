package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"paperqual/internal/application"
	"paperqual/internal/domain"
)

func (s *Server) HandleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "route_not_found", "请求的 API 路由不存在", 0)
}

func (s *Server) HandleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.service.Ready(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error(), 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": true})
}
func (s *Server) HandleGetBatch(w http.ResponseWriter, r *http.Request) {
	view, err := s.service.GetBatch(r.PathValue("batch_id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
func (s *Server) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	query, err := parseTimelineQuery(r.URL.Query())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	page, err := s.service.QueryTimelineContext(r.Context(), r.PathValue("batch_id"), query)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func parseTimelineQuery(values url.Values) (application.TimelineQuery, error) {
	allowed := map[string]bool{"cursor": true, "limit": true, "page_size": true, "event_type": true, "actor_id": true, "actor": true, "min_revision": true, "revision_from": true, "max_revision": true, "revision_to": true, "snapshot_anchor": true, "anchor": true}
	for key := range values {
		if !allowed[key] {
			return application.TimelineQuery{}, domain.Errorf(domain.CodeInvalidQuery, "未知时间线查询参数 %s", key)
		}
		if len(values[key]) != 1 {
			return application.TimelineQuery{}, domain.Errorf(domain.CodeInvalidQuery, "查询参数 %s 只能出现一次", key)
		}
	}
	query := application.TimelineQuery{Limit: 50}
	if raw, ok, err := queryValue(values, "cursor"); err != nil {
		return query, err
	} else if ok {
		value, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			return query, domain.Errorf(domain.CodeInvalidQuery, "cursor 必须是非负事件序号")
		}
		query.Cursor = &value
	}
	if raw, ok, err := queryValue(values, "limit", "page_size"); err != nil {
		return query, err
	} else if ok {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			return query, domain.Errorf(domain.CodeInvalidQuery, "limit 必须是整数")
		}
		query.Limit = value
	}
	if raw, ok, err := queryValue(values, "event_type"); err != nil {
		return query, err
	} else if ok {
		query.EventType = raw
	}
	if raw, ok, err := queryValue(values, "actor_id", "actor"); err != nil {
		return query, err
	} else if ok {
		query.ActorID = raw
	}
	if raw, ok, err := queryInt64(values, "min_revision", "revision_from"); err != nil {
		return query, err
	} else if ok {
		query.MinRevision = &raw
	}
	if raw, ok, err := queryInt64(values, "max_revision", "revision_to"); err != nil {
		return query, err
	} else if ok {
		query.MaxRevision = &raw
	}
	if raw, ok, err := queryValue(values, "snapshot_anchor", "anchor"); err != nil {
		return query, err
	} else if ok {
		if !domain.ValidDigest(raw) {
			return query, domain.Errorf(domain.CodeInvalidQuery, "snapshot_anchor 必须是 SHA-256 摘要")
		}
		query.SnapshotAnchor = raw
	}
	return query, nil
}

func queryValue(values url.Values, names ...string) (string, bool, error) {
	found := ""
	count := 0
	for _, name := range names {
		if value, ok := values[name]; ok {
			found = strings.TrimSpace(value[0])
			count++
		}
	}
	if count > 1 {
		return "", false, domain.Errorf(domain.CodeInvalidQuery, "同一查询条件不能使用多个别名")
	}
	if count == 1 && found == "" {
		return "", false, domain.Errorf(domain.CodeInvalidQuery, "查询参数不能为空")
	}
	return found, count == 1, nil
}

func queryInt64(values url.Values, names ...string) (int64, bool, error) {
	raw, ok, err := queryValue(values, names...)
	if err != nil || !ok {
		return 0, ok, err
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, domain.Errorf(domain.CodeInvalidQuery, "%s 必须是整数", names[0])
	}
	return value, true, nil
}
func (s *Server) HandleCertificate(w http.ResponseWriter, r *http.Request) {
	raw, err := s.service.Certificate(r.PathValue("batch_id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeRaw(w, http.StatusOK, raw, false)
}
func (s *Server) HandleVerifyCertificate(w http.ResponseWriter, r *http.Request) {
	report, err := s.service.VerifyCertificate(r.PathValue("batch_id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
