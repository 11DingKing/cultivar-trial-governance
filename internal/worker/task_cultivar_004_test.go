package worker

import (
 "errors"
 "context"
 "testing"
 "github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func TestTransientWorkerFailureRemainsRetryable(t *testing.T) {
 db, manual, runner := workerFixture(t); insertWorkerJob(t, db, manual.Now(), "task-transient", domain.JobAdoptionFollowUp); runner.Handlers[domain.JobAdoptionFollowUp] = HandlerFunc(func(_ context.Context, _ domain.WorkerJob) error { return errors.New("temporary storage failure") }); _ = runner.RunOne(context.Background()); job, err := db.GetJob(context.Background(), "task-transient"); if err != nil { t.Fatal(err) }; if job.Status == domain.JobDead { t.Fatalf("transient failure became terminal: %+v", job) }
}
