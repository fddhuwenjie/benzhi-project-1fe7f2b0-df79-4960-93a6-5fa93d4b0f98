package domain

import "fmt"

type Error struct {
	Code            string        `json:"code"`
	Message         string        `json:"message"`
	CurrentRevision int64         `json:"current_revision"`
	Details         []ErrorDetail `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func NewError(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Errorf(code, format string, args ...any) *Error {
	return NewError(code, fmt.Sprintf(format, args...))
}

type ErrorDetail struct {
	Index   *int   `json:"index,omitempty"`
	ItemID  string `json:"item_id,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewDetailedError(code, message string, details []ErrorDetail) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

func WithRevision(err error, revision int64) error {
	if de, ok := err.(*Error); ok {
		copy := *de
		copy.CurrentRevision = revision
		return &copy
	}
	return err
}

const (
	CodeValidation             = "validation_failed"
	CodeNotFound               = "batch_not_found"
	CodeDuplicateItem          = "duplicate_item"
	CodeInvalidState           = "invalid_state"
	CodeRevisionConflict       = "revision_conflict"
	CodeIdempotency            = "idempotency_conflict"
	CodeIndependence           = "reviewer_not_independent"
	CodeEvidenceCorrupt        = "evidence_corrupt"
	CodeReadOnly               = "batch_read_only"
	CodeRequestTooLarge        = "request_too_large"
	CodeInvalidJSON            = "invalid_json"
	CodeBulkItemsInvalid       = "bulk_items_invalid"
	CodeBulkCorrectionsInvalid = "bulk_corrections_invalid"
	CodeInvalidQuery           = "invalid_query"
	CodeTimelineChanged        = "timeline_changed"
)
