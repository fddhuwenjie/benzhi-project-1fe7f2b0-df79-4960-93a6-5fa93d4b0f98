package evidence

import (
	"encoding/json"
	"fmt"
	"sort"

	"paperqual/internal/domain"
)

type CertificateStandard struct {
	TargetSurfacePHMin    float64 `json:"target_surface_ph_min"`
	TargetSurfacePHMax    float64 `json:"target_surface_ph_max"`
	MinAlkalineReservePct float64 `json:"min_alkaline_reserve_pct"`
	MaxColorDeltaE        float64 `json:"max_color_delta_e"`
	SampleRatio           float64 `json:"sample_ratio"`
}

type CertificateItem struct {
	ItemID              string   `json:"item_id"`
	ShelfMark           string   `json:"shelf_mark"`
	PaperType           string   `json:"paper_type"`
	LatestRoundID       string   `json:"latest_round_id"`
	QualificationStatus string   `json:"qualification_status"`
	FailureCodes        []string `json:"failure_codes"`
}

type CertificateReview struct {
	ReviewID        string                      `json:"review_id"`
	SampleSeed      string                      `json:"sample_seed"`
	SampledItemIDs  []string                    `json:"sampled_item_ids"`
	ReviewerID      string                      `json:"reviewer_id"`
	ItemDecisions   []domain.ReviewItemDecision `json:"item_decisions"`
	OverallDecision string                      `json:"overall_decision"`
	SignedAt        string                      `json:"signed_at"`
}

type Certificate struct {
	Schema         string              `json:"schema"`
	BatchID        string              `json:"batch_id"`
	Title          string              `json:"title"`
	Revision       int64               `json:"revision"`
	BaselineDigest string              `json:"baseline_digest"`
	Standards      CertificateStandard `json:"standards"`
	Items          []CertificateItem   `json:"items"`
	Review         CertificateReview   `json:"review"`
	FinalDecision  string              `json:"final_decision"`
	SealedAt       string              `json:"sealed_at"`
	AuditAnchor    string              `json:"audit_anchor"`
}

type Envelope struct {
	Certificate Certificate `json:"certificate"`
	Digest      string      `json:"sha256"`
}

type IntegrityReport struct {
	Valid              bool   `json:"valid"`
	CertificateDigest  string `json:"certificate_digest"`
	BatchIDMatches     bool   `json:"batch_id_matches"`
	RevisionMatches    bool   `json:"revision_matches"`
	AuditAnchorMatches bool   `json:"audit_anchor_matches"`
	Message            string `json:"message"`
}

func BuildCertificate(b *domain.TreatmentBatch) (Envelope, []byte, error) {
	if err := ValidateCertificateSource(b); err != nil {
		return Envelope{}, nil, err
	}
	items := make([]CertificateItem, 0, len(b.Items))
	for _, item := range b.Items {
		items = append(items, CertificateItem{ItemID: item.ItemID, ShelfMark: item.ShelfMark, PaperType: item.PaperType, LatestRoundID: item.LatestRoundID, QualificationStatus: item.QualificationStatus, FailureCodes: append([]string(nil), item.FailureCodes...)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ItemID < items[j].ItemID })
	decisions := append([]domain.ReviewItemDecision(nil), b.Review.ItemDecisions...)
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].ItemID < decisions[j].ItemID })
	sampled := append([]string(nil), b.Review.SampledItemIDs...)
	sort.Strings(sampled)
	cert := Certificate{Schema: "paperqual-certificate-v1", BatchID: b.BatchID, Title: b.Title, Revision: b.Revision, BaselineDigest: b.BaselineDigest,
		Standards: CertificateStandard{TargetSurfacePHMin: b.Standards.TargetSurfacePHMin, TargetSurfacePHMax: b.Standards.TargetSurfacePHMax, MinAlkalineReservePct: b.Standards.MinAlkalineReservePct, MaxColorDeltaE: b.Standards.MaxColorDeltaE, SampleRatio: b.Standards.SampleRatio},
		Items:     items, Review: CertificateReview{ReviewID: b.Review.ReviewID, SampleSeed: b.Review.SampleSeed, SampledItemIDs: sampled, ReviewerID: b.Review.ReviewerID, ItemDecisions: decisions, OverallDecision: b.Review.OverallDecision, SignedAt: Time(*b.Review.SignedAt)}, FinalDecision: b.FinalDecision, SealedAt: Time(*b.SealedAt), AuditAnchor: b.AuditAnchor}
	digest, _, err := Digest(cert)
	if err != nil {
		return Envelope{}, nil, err
	}
	envelope := Envelope{Certificate: cert, Digest: digest}
	raw, err := CanonicalJSON(envelope)
	if err != nil {
		return Envelope{}, nil, err
	}
	return envelope, raw, nil
}

func ValidateCertificateSource(b *domain.TreatmentBatch) error {
	if b == nil {
		return fmt.Errorf("批次不能为空")
	}
	if !b.Status.Terminal() || b.SealedAt == nil {
		return domain.Errorf(domain.CodeInvalidState, "只有已封存终态批次可以生成证书")
	}
	if !domain.ValidDigest(b.BaselineDigest) {
		return domain.Errorf(domain.CodeEvidenceCorrupt, "冻结基线摘要无效")
	}
	if !domain.ValidDigest(b.AuditAnchor) {
		return domain.Errorf(domain.CodeEvidenceCorrupt, "审计链锚点无效")
	}
	if b.Review == nil || b.Review.SignedAt == nil {
		return domain.Errorf(domain.CodeInvalidState, "缺少已签署独立抽检")
	}
	if b.Review.ReviewerID != b.ReviewerID || b.Review.ReviewerID == b.OperatorID {
		return domain.Errorf(domain.CodeIndependence, "证书复核角色不满足独立性")
	}
	if len(b.Review.SampledItemIDs) == 0 || len(b.Review.ItemDecisions) != len(b.Review.SampledItemIDs) {
		return domain.Errorf(domain.CodeEvidenceCorrupt, "抽检清单或逐项决定不完整")
	}
	sampled := make(map[string]bool, len(b.Review.SampledItemIDs))
	for _, id := range b.Review.SampledItemIDs {
		if sampled[id] {
			return domain.Errorf(domain.CodeEvidenceCorrupt, "抽检清单包含重复件号")
		}
		if _, ok := b.FindItem(id); !ok {
			return domain.Errorf(domain.CodeEvidenceCorrupt, "抽检清单引用未知档案件")
		}
		sampled[id] = true
	}
	accepted := true
	decided := make(map[string]bool, len(b.Review.ItemDecisions))
	for _, decision := range b.Review.ItemDecisions {
		if !sampled[decision.ItemID] || decided[decision.ItemID] {
			return domain.Errorf(domain.CodeEvidenceCorrupt, "抽检决定范围无效")
		}
		decided[decision.ItemID] = true
		if decision.Decision == "reject" {
			accepted = false
		} else if decision.Decision != "accept" {
			return domain.Errorf(domain.CodeEvidenceCorrupt, "抽检决定值无效")
		}
	}
	if b.FinalDecision == "release" {
		if !accepted || b.Review.OverallDecision != "accepted" || b.Status != domain.StatusReleased {
			return domain.Errorf(domain.CodeEvidenceCorrupt, "放行结论与抽检结果不一致")
		}
		for _, item := range b.Items {
			if item.QualificationStatus != "qualified" || len(item.FailureCodes) != 0 {
				return domain.Errorf(domain.CodeEvidenceCorrupt, "放行证书包含不合格档案件")
			}
		}
	} else if b.FinalDecision == "reject" {
		if accepted || b.Review.OverallDecision != "rejected" || b.Status != domain.StatusRejected {
			return domain.Errorf(domain.CodeEvidenceCorrupt, "拒绝结论与抽检结果不一致")
		}
	} else {
		return domain.Errorf(domain.CodeEvidenceCorrupt, "终局决定无效")
	}
	return nil
}

func VerifyCertificate(raw []byte, b *domain.TreatmentBatch) IntegrityReport {
	report := IntegrityReport{Message: "证书完整"}
	var envelope Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		report.Message = "证书 JSON 无效"
		return report
	}
	digest, _, err := Digest(envelope.Certificate)
	if err != nil {
		report.Message = "无法计算证书摘要"
		return report
	}
	report.CertificateDigest = digest
	report.BatchIDMatches = envelope.Certificate.BatchID == b.BatchID
	report.RevisionMatches = envelope.Certificate.Revision == b.Revision
	report.AuditAnchorMatches = envelope.Certificate.AuditAnchor == b.AuditAnchor
	if digest != envelope.Digest {
		report.Message = "证书内容摘要不匹配"
		return report
	}
	if b.CertificateDigest != "" && digest != b.CertificateDigest {
		report.Message = "证书与批次摘要不匹配"
		return report
	}
	if !report.BatchIDMatches || !report.RevisionMatches || !report.AuditAnchorMatches {
		report.Message = "证书身份、修订号或审计锚点不匹配"
		return report
	}
	report.Valid = true
	return report
}
