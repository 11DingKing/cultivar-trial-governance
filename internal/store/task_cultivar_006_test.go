package store

import (
 "context"
 "testing"
 "time"
 "github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func TestReviewQuotaCountsEveryDecision(t *testing.T) {
 db := openTestDB(t); now := fixtureTime(); first := domain.ExpertReview{ID:"review-one", ApplicationID:"app-quota", ExpertUserID:"expert-one", InstitutionID:"inst-one", Decision:domain.ReviewReject, Rationale:"否决意见包含足够的审定依据和风险说明", PolicyRef:"P", SubmittedAt:now}; second := first; second.ID="review-two"; second.ExpertUserID="expert-two"; if err := db.InsertExpertReview(context.Background(), nil, first, 1); err != nil { t.Fatal(err) }; if err := db.InsertExpertReview(context.Background(), nil, second, 1); err == nil { t.Fatalf("second review bypassed quota: %v", err) }; _ = time.Second
}
