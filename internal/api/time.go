package api

import (
	"encoding/json"
	"fmt"
	"time"
)

type timeValue struct{ time.Time }

func (t *timeValue) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return fmt.Errorf("时间必须是 RFC3339 字符串")
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return fmt.Errorf("时间必须是 RFC3339 格式")
	}
	t.Time = parsed.UTC()
	return nil
}
