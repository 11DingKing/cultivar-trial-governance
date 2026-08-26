CREATE INDEX idx_worker_jobs_recovery
    ON worker_jobs(status, lease_expires_at, attempts)
    WHERE status = 'running';

CREATE INDEX idx_observation_batches_reminders
    ON observation_batches(status, closes_at, application_id)
    WHERE status = 'open';

CREATE INDEX idx_regional_adoptions_follow_up
    ON regional_adoptions(status, adopted_at, region)
    WHERE status = 'active';
