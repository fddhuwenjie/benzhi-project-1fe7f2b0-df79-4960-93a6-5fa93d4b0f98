package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"sort"
	"time"
)

func (b *TreatmentBatch) StartReview(reviewID, actorID string) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusReviewReady {
		return Errorf(CodeInvalidState, "批次尚不具备抽检资格")
	}
	if actorID != b.ReviewerID || actorID == b.OperatorID {
		return Errorf(CodeIndependence, "抽检必须由指定独立复核员执行")
	}
	if err := ValidateIdentifier("review_id", reviewID); err != nil {
		return err
	}
	seed, ids := DeterministicSample(b)
	b.Review = &QualityReview{ReviewID: reviewID, BatchID: b.BatchID, SampleSeed: seed, SampledItemIDs: ids, ReviewerID: actorID, ItemDecisions: []ReviewItemDecision{}, RejectionReasons: []string{}}
	b.Status = StatusInReview
	return nil
}

func DeterministicSample(b *TreatmentBatch) (string, []string) {
	seedBytes := sha256.Sum256([]byte(b.BaselineDigest + "|" + b.BatchID))
	seed := hex.EncodeToString(seedBytes[:])
	type ranked struct {
		id    string
		score [32]byte
	}
	ranks := make([]ranked, 0, len(b.Items))
	for _, item := range b.Items {
		ranks = append(ranks, ranked{id: item.ItemID, score: sha256.Sum256([]byte(seed + "|" + item.ItemID))})
	}
	sort.Slice(ranks, func(i, j int) bool {
		return binary.BigEndian.Uint64(ranks[i].score[:8]) < binary.BigEndian.Uint64(ranks[j].score[:8])
	})
	count := int(math.Ceil(float64(len(ranks)) * b.Standards.SampleRatio))
	if count < 1 {
		count = 1
	}
	ids := make([]string, count)
	for i := 0; i < count; i++ {
		ids[i] = ranks[i].id
	}
	sort.Strings(ids)
	return seed, ids
}

func (b *TreatmentBatch) SubmitReview(actorID string, decisions []ReviewItemDecision, signedAt time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusInReview || b.Review == nil {
		return Errorf(CodeInvalidState, "当前没有待签署抽检任务")
	}
	if actorID != b.ReviewerID || actorID != b.Review.ReviewerID || actorID == b.OperatorID {
		return Errorf(CodeIndependence, "抽检签署人不满足独立性约束")
	}
	if len(decisions) != len(b.Review.SampledItemIDs) {
		return Errorf(CodeValidation, "必须提交完整抽检清单")
	}
	expected := map[string]bool{}
	for _, id := range b.Review.SampledItemIDs {
		expected[id] = true
	}
	seen := map[string]bool{}
	rejected := false
	reasons := []string{}
	for _, d := range decisions {
		if !expected[d.ItemID] || seen[d.ItemID] {
			return Errorf(CodeValidation, "抽检决定包含重复或非抽样 item_id")
		}
		seen[d.ItemID] = true
		if d.Decision != "accept" && d.Decision != "reject" {
			return Errorf(CodeValidation, "抽检决定必须是 accept 或 reject")
		}
		if d.Decision == "reject" {
			if err := ValidateText("rejection reason", d.Reason, 1000); err != nil {
				return err
			}
			rejected = true
			reasons = append(reasons, d.ItemID+":"+d.Reason)
		}
	}
	b.Review.ItemDecisions = append([]ReviewItemDecision(nil), decisions...)
	b.Review.RejectionReasons = reasons
	b.Review.SignedAt = timePointer(signedAt.UTC())
	if rejected {
		b.Review.OverallDecision = "rejected"
	} else {
		b.Review.OverallDecision = "accepted"
	}
	return nil
}

func (b *TreatmentBatch) Seal(decision, actorID string, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusInReview || b.Review == nil || b.Review.SignedAt == nil {
		return Errorf(CodeInvalidState, "抽检尚未完整签署")
	}
	if actorID != b.ReviewerID {
		return Errorf(CodeIndependence, "终局决定必须由指定复核员签署")
	}
	if decision == "release" && b.Review.OverallDecision != "accepted" {
		return Errorf(CodeInvalidState, "存在抽检驳回时不能放行")
	}
	if decision == "reject" && b.Review.OverallDecision != "rejected" {
		return Errorf(CodeInvalidState, "抽检全部接受时不能拒绝")
	}
	if decision != "release" && decision != "reject" {
		return Errorf(CodeValidation, "decision 必须是 release 或 reject")
	}
	b.FinalDecision = decision
	sealed := now.UTC()
	b.SealedAt = &sealed
	if decision == "release" {
		b.Status = StatusReleased
	} else {
		b.Status = StatusRejected
	}
	return nil
}

func timePointer(t time.Time) *time.Time { return &t }
