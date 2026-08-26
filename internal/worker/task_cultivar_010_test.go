package worker

import (
 "context"
 "testing"
 "github.com/11DingKing/cultivar-trial-governance/internal/apperror"
 "github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func TestWorkerCompletionRequiresLeaseOwner(t *testing.T) {
 db, manual, runner := workerFixture(t); insertWorkerJob(t, db, manual.Now(), "lease-job", domain.JobAdoptionFollowUp); runner.Handlers[domain.JobAdoptionFollowUp] = HandlerFunc(func(context.Context, domain.WorkerJob) error { return apperror.ErrPermanent }); _ = runner.RunOne(context.Background()); t.Fatalf("error cause lost: %v", apperror.ErrPermanent)
}
