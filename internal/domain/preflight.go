package domain

type RoundItemResult struct {
	ItemID       string   `json:"item_id"`
	FailureCodes []string `json:"failure_codes"`
	Result       string   `json:"result"`
}

type RoundPreview struct {
	RoundID        string            `json:"round_id"`
	RoundKind      string            `json:"round_kind"`
	OverallResult  string            `json:"overall_result"`
	ExpectedStatus BatchStatus       `json:"expected_status"`
	Items          []RoundItemResult `json:"items"`
}

// PreviewRound intentionally runs the production transition on an isolated copy.
func (b *TreatmentBatch) PreviewRound(round TreatmentRound) (RoundPreview, error) {
	copy := b.evaluationCopy()
	if err := copy.SubmitRound(round); err != nil {
		return RoundPreview{}, err
	}
	committed := copy.Rounds[len(copy.Rounds)-1]
	results := make([]RoundItemResult, 0, len(committed.Measurements))
	for _, measurement := range committed.Measurements {
		result := "qualified"
		if len(measurement.FailureCodes) > 0 {
			result = "failed"
		}
		results = append(results, RoundItemResult{ItemID: measurement.ItemID, FailureCodes: append([]string(nil), measurement.FailureCodes...), Result: result})
	}
	return RoundPreview{RoundID: committed.RoundID, RoundKind: committed.RoundKind, OverallResult: committed.Result, ExpectedStatus: copy.Status, Items: results}, nil
}

func (b *TreatmentBatch) evaluationCopy() *TreatmentBatch {
	copy := *b
	copy.Items = append([]ArchiveItem(nil), b.Items...)
	for i := range copy.Items {
		copy.Items[i].FailureCodes = append([]string(nil), b.Items[i].FailureCodes...)
	}
	copy.Rounds = append([]TreatmentRound(nil), b.Rounds...)
	copy.Corrections = append([]Correction(nil), b.Corrections...)
	return &copy
}
