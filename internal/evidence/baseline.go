package evidence

import (
	"sort"

	"paperqual/internal/domain"
)

type BaselineItem struct {
	ItemID                string  `json:"item_id"`
	ShelfMark             string  `json:"shelf_mark"`
	PaperType             string  `json:"paper_type"`
	BaselineSurfacePH     float64 `json:"baseline_surface_ph"`
	BaselineColdExtractPH float64 `json:"baseline_cold_extract_ph"`
	MeasurementPoints     int     `json:"measurement_points"`
	SourceDigest          string  `json:"source_digest"`
}

type BaselineRecord struct {
	Schema     string              `json:"schema"`
	BatchID    string              `json:"batch_id"`
	Title      string              `json:"title"`
	OperatorID string              `json:"operator_id"`
	ReviewerID string              `json:"reviewer_id"`
	Standards  CertificateStandard `json:"standards"`
	Items      []BaselineItem      `json:"items"`
}

func BuildBaseline(batch *domain.TreatmentBatch) (BaselineRecord, string, error) {
	items := make([]BaselineItem, 0, len(batch.Items))
	for _, item := range batch.Items {
		items = append(items, BaselineItem{
			ItemID:                item.ItemID,
			ShelfMark:             item.ShelfMark,
			PaperType:             item.PaperType,
			BaselineSurfacePH:     item.BaselineSurfacePH,
			BaselineColdExtractPH: item.BaselineColdExtractPH,
			MeasurementPoints:     item.MeasurementPoints,
			SourceDigest:          item.SourceDigest,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ItemID < items[j].ItemID })
	record := BaselineRecord{
		Schema:     "paperqual-baseline-v1",
		BatchID:    batch.BatchID,
		Title:      batch.Title,
		OperatorID: batch.OperatorID,
		ReviewerID: batch.ReviewerID,
		Standards: CertificateStandard{
			TargetSurfacePHMin:    batch.Standards.TargetSurfacePHMin,
			TargetSurfacePHMax:    batch.Standards.TargetSurfacePHMax,
			MinAlkalineReservePct: batch.Standards.MinAlkalineReservePct,
			MaxColorDeltaE:        batch.Standards.MaxColorDeltaE,
			SampleRatio:           batch.Standards.SampleRatio,
		},
		Items: items,
	}
	digest, _, err := Digest(record)
	return record, digest, err
}
