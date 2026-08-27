package domain

import (
	"encoding/hex"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidateIdentifier(field, value string) error {
	if !identifierPattern.MatchString(value) {
		return Errorf(CodeValidation, "%s 必须是 1 到 128 位字母、数字、点、下划线或连字符", field)
	}
	return nil
}

func ValidateText(field, value string, maxRunes int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Errorf(CodeValidation, "%s 不能为空", field)
	}
	if !utf8.ValidString(value) {
		return Errorf(CodeValidation, "%s 必须是有效 UTF-8", field)
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return Errorf(CodeValidation, "%s 不能超过 %d 个字符", field, maxRunes)
	}
	return nil
}

func ValidateStandards(s Standards) error {
	if !finite(s.TargetSurfacePHMin) || s.TargetSurfacePHMin < 3 || s.TargetSurfacePHMin > 12 {
		return Errorf(CodeValidation, "target_surface_ph_min 必须在 3 到 12 之间")
	}
	if !finite(s.TargetSurfacePHMax) || s.TargetSurfacePHMax <= s.TargetSurfacePHMin || s.TargetSurfacePHMax > 12 {
		return Errorf(CodeValidation, "target_surface_ph_max 必须大于下限且不超过 12")
	}
	if !finite(s.MinAlkalineReservePct) || s.MinAlkalineReservePct < 0 || s.MinAlkalineReservePct > 20 {
		return Errorf(CodeValidation, "min_alkaline_reserve_pct 必须在 0 到 20 之间")
	}
	if !finite(s.MaxColorDeltaE) || s.MaxColorDeltaE <= 0 || s.MaxColorDeltaE > 20 {
		return Errorf(CodeValidation, "max_color_delta_e 必须在 0 到 20 之间")
	}
	if !finite(s.SampleRatio) || s.SampleRatio <= 0 || s.SampleRatio > 1 {
		return Errorf(CodeValidation, "sample_ratio 必须大于 0 且不超过 1")
	}
	return nil
}

func ValidateItem(item ArchiveItem) error {
	if err := ValidateIdentifier("item_id", item.ItemID); err != nil {
		return err
	}
	if err := ValidateText("shelf_mark", item.ShelfMark, 128); err != nil {
		return err
	}
	if err := ValidateText("paper_type", item.PaperType, 128); err != nil {
		return err
	}
	if !validPH(item.BaselineSurfacePH) || !validPH(item.BaselineColdExtractPH) {
		return Errorf(CodeValidation, "处理前酸碱度必须在 0 到 14 之间")
	}
	if item.MeasurementPoints < 1 || item.MeasurementPoints > 1000 {
		return Errorf(CodeValidation, "measurement_points 必须在 1 到 1000 之间")
	}
	if !ValidDigest(item.SourceDigest) {
		return Errorf(CodeValidation, "source_digest 必须是 64 位十六进制 SHA-256")
	}
	return nil
}

func ValidateMeasurement(m Measurement) error {
	if err := ValidateIdentifier("measurement.item_id", m.ItemID); err != nil {
		return err
	}
	if !validPH(m.SurfacePH) || !validPH(m.ColdExtractPH) {
		return Errorf(CodeValidation, "处理后酸碱度必须在 0 到 14 之间")
	}
	if !finite(m.AlkalineReservePct) || m.AlkalineReservePct < 0 || m.AlkalineReservePct > 100 {
		return Errorf(CodeValidation, "alkaline_reserve_pct 必须在 0 到 100 之间")
	}
	if !finite(m.ColorDeltaE) || m.ColorDeltaE < 0 || m.ColorDeltaE > 100 {
		return Errorf(CodeValidation, "color_delta_e 必须在 0 到 100 之间")
	}
	if !ValidDigest(m.SourceDigest) {
		return Errorf(CodeValidation, "measurement.source_digest 必须是 SHA-256")
	}
	return nil
}

func ValidDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func validPH(v float64) bool { return finite(v) && v >= 0 && v <= 14 }
func finite(v float64) bool  { return !math.IsNaN(v) && !math.IsInf(v, 0) }
