package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"paperqual/internal/application"
	"paperqual/internal/domain"
)

const maxBodyBytes = 1 << 20

type commandFields struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type emptyCommand struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type errorBody struct {
	Error struct {
		Code            string               `json:"code"`
		Message         string               `json:"message"`
		CurrentRevision int64                `json:"current_revision"`
		Details         []domain.ErrorDetail `json:"details,omitempty"`
	} `json:"error"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return domain.Errorf(domain.CodeInvalidJSON, "Content-Type 必须是 application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return domain.Errorf(domain.CodeRequestTooLarge, "请求体不能超过 1 MiB")
		}
		return domain.Errorf(domain.CodeInvalidJSON, "JSON 请求无效：%v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return domain.Errorf(domain.CodeInvalidJSON, "请求体只能包含一个 JSON 对象")
	}
	return nil
}

func meta(r *http.Request, requestID string, expected int64) application.CommandMeta {
	return application.CommandMeta{RequestID: requestID, ExpectedRevision: expected, ActorID: strings.TrimSpace(r.Header.Get("X-Actor-ID"))}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeRaw(w http.ResponseWriter, status int, raw []byte, replayed bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if replayed {
		w.Header().Set("X-Idempotent-Replay", "true")
	}
	w.WriteHeader(status)
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n"))
}

func writeDomainError(w http.ResponseWriter, err error) {
	de, ok := err.(*domain.Error)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "服务内部错误", 0)
		return
	}
	status := http.StatusUnprocessableEntity
	switch de.Code {
	case domain.CodeNotFound:
		status = http.StatusNotFound
	case domain.CodeRevisionConflict, domain.CodeIdempotency, domain.CodeDuplicateItem, domain.CodeTimelineChanged:
		status = http.StatusConflict
	case domain.CodeReadOnly, domain.CodeInvalidState:
		status = http.StatusConflict
	case domain.CodeIndependence:
		status = http.StatusForbidden
	case domain.CodeRequestTooLarge:
		status = http.StatusRequestEntityTooLarge
	case domain.CodeInvalidJSON:
		status = http.StatusBadRequest
	case domain.CodeInvalidQuery:
		status = http.StatusBadRequest
	case domain.CodeEvidenceCorrupt:
		status = http.StatusInternalServerError
	}
	writeErrorDetails(w, status, de.Code, de.Message, de.CurrentRevision, de.Details)
}

func writeError(w http.ResponseWriter, status int, code, message string, revision int64) {
	writeErrorDetails(w, status, code, message, revision, nil)
}

func writeErrorDetails(w http.ResponseWriter, status int, code, message string, revision int64, details []domain.ErrorDetail) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	body.Error.CurrentRevision = revision
	body.Error.Details = details
	writeJSON(w, status, body)
}
