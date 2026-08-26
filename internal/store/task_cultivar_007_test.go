package store

import (
 "context"
 "testing"
 "time"
 "github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func TestPublishedConclusionCannotBePublishedAgain(t *testing.T) {
 db := openTestDB(t); now := fixtureTime(); c := domain.Conclusion{ID:"conclusion-published", ApplicationID:"app-publish", Version:1, Status:domain.ConclusionDraft, Decision:domain.ReviewRecommend, Summary:"这是一个足够完整的区域试验审定结论摘要", PolicyRef:"P", CreatedAt:now}; if err := db.InsertConclusion(context.Background(), nil, c); err != nil { t.Fatal(err) }; if err := db.PublishConclusion(context.Background(), nil, c.ID, "expert-1", now.Format(time.RFC3339)); err != nil { t.Fatal(err) }; if err := db.PublishConclusion(context.Background(), nil, c.ID, "expert-2", now.Add(time.Hour).Format(time.RFC3339)); err == nil { t.Fatalf("republishing published conclusion unexpectedly succeeded: %v", err) }
}
