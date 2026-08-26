PRAGMA foreign_keys = ON;

CREATE TABLE institutions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    region TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    created_at TEXT NOT NULL
);

CREATE INDEX idx_institutions_region_active ON institutions(region, active);

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    institution_id TEXT NOT NULL REFERENCES institutions(id),
    email TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('station_staff','breeder','review_expert','seed_custodian','regional_planner','admin')),
    region TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_users_institution_role ON users(institution_id, role, active);
CREATE INDEX idx_users_region_role ON users(region, role, active);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_sessions_user_active ON sessions(user_id, revoked_at, expires_at);

CREATE TABLE varieties (
    id TEXT PRIMARY KEY,
    owner_institution_id TEXT NOT NULL REFERENCES institutions(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    crop TEXT NOT NULL,
    generation INTEGER NOT NULL CHECK (generation > 0),
    traits_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    UNIQUE(owner_institution_id, code)
);

CREATE INDEX idx_varieties_crop_name ON varieties(crop, name);

CREATE TABLE applications (
    id TEXT PRIMARY KEY,
    variety_id TEXT NOT NULL REFERENCES varieties(id),
    applicant_user_id TEXT NOT NULL REFERENCES users(id),
    applicant_institution_id TEXT NOT NULL REFERENCES institutions(id),
    region TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('submitted','qualified','plan_approved','allocated','running','interrupted','data_locked','under_review','published','adopted','rejected','cancelled','revoked')),
    policy_ref TEXT NOT NULL,
    submission_note TEXT NOT NULL,
    qualification_note TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    submitted_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_applications_region_status ON applications(region, status, updated_at);
CREATE INDEX idx_applications_institution_status ON applications(applicant_institution_id, status);
CREATE INDEX idx_applications_variety ON applications(variety_id, submitted_at);

CREATE TABLE seed_lots (
    id TEXT PRIMARY KEY,
    variety_id TEXT NOT NULL REFERENCES varieties(id),
    institution_id TEXT NOT NULL REFERENCES institutions(id),
    lot_code TEXT NOT NULL,
    quantity_grams INTEGER NOT NULL CHECK (quantity_grams >= 0),
    reserved_grams INTEGER NOT NULL DEFAULT 0 CHECK (reserved_grams >= 0 AND reserved_grams <= quantity_grams),
    expires_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(institution_id, lot_code)
);

CREATE INDEX idx_seed_lots_variety_expiry ON seed_lots(variety_id, expires_at);

CREATE TABLE trial_sites (
    id TEXT PRIMARY KEY,
    institution_id TEXT NOT NULL REFERENCES institutions(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    timezone TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE(institution_id, code)
);

CREATE INDEX idx_trial_sites_region_active ON trial_sites(region, active);

CREATE TABLE plot_seasons (
    id TEXT PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES trial_sites(id),
    plot_code TEXT NOT NULL,
    season TEXT NOT NULL,
    area_square_m INTEGER NOT NULL CHECK (area_square_m > 0),
    status TEXT NOT NULL CHECK (status IN ('available','reserved','in_use','released')),
    application_id TEXT REFERENCES applications(id),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(site_id, plot_code, season)
);

CREATE INDEX idx_plot_seasons_available ON plot_seasons(season, status, site_id);
CREATE UNIQUE INDEX idx_plot_seasons_application_active
    ON plot_seasons(application_id)
    WHERE application_id IS NOT NULL AND status IN ('reserved','in_use');

CREATE TABLE trial_plans (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL UNIQUE REFERENCES applications(id),
    season TEXT NOT NULL,
    region TEXT NOT NULL,
    observation_opens_at TEXT NOT NULL,
    observation_closes_at TEXT NOT NULL,
    required_reviewers INTEGER NOT NULL CHECK (required_reviewers >= 2),
    max_reviewers INTEGER NOT NULL CHECK (max_reviewers >= required_reviewers),
    status TEXT NOT NULL CHECK (status IN ('draft','approved','executing','locked','cancelled')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (observation_opens_at < observation_closes_at)
);

CREATE INDEX idx_trial_plans_season_status ON trial_plans(season, status);

CREATE TABLE allocations (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL UNIQUE REFERENCES applications(id),
    seed_lot_id TEXT NOT NULL REFERENCES seed_lots(id),
    plot_season_id TEXT NOT NULL UNIQUE REFERENCES plot_seasons(id),
    seed_grams INTEGER NOT NULL CHECK (seed_grams > 0),
    status TEXT NOT NULL CHECK (status IN ('reserved','consumed','released')),
    allocated_by TEXT NOT NULL REFERENCES users(id),
    allocated_at TEXT NOT NULL,
    released_at TEXT
);

CREATE INDEX idx_allocations_seed_status ON allocations(seed_lot_id, status);

CREATE TABLE observation_batches (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id),
    window_name TEXT NOT NULL,
    opens_at TEXT NOT NULL,
    closes_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('open','locked','cancelled')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    locked_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(application_id, window_name),
    CHECK (opens_at < closes_at)
);

CREATE INDEX idx_observation_batches_open ON observation_batches(status, closes_at);

CREATE TABLE observations (
    id TEXT PRIMARY KEY,
    batch_id TEXT NOT NULL REFERENCES observation_batches(id),
    metric TEXT NOT NULL,
    value REAL NOT NULL,
    unit TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    reported_by TEXT NOT NULL REFERENCES users(id),
    anomalous INTEGER NOT NULL DEFAULT 0 CHECK (anomalous IN (0, 1)),
    invalidated INTEGER NOT NULL DEFAULT 0 CHECK (invalidated IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE(batch_id, metric, observed_at)
);

CREATE INDEX idx_observations_batch_anomaly ON observations(batch_id, anomalous, invalidated);

CREATE TABLE expert_reviews (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id),
    expert_user_id TEXT NOT NULL REFERENCES users(id),
    institution_id TEXT NOT NULL REFERENCES institutions(id),
    decision TEXT NOT NULL CHECK (decision IN ('recommend','reject','revise')),
    rationale TEXT NOT NULL,
    policy_ref TEXT NOT NULL,
    submitted_at TEXT NOT NULL,
    UNIQUE(application_id, expert_user_id)
);

CREATE INDEX idx_expert_reviews_application_decision ON expert_reviews(application_id, decision);

CREATE TABLE conclusions (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id),
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft','published','superseded','revoked')),
    decision TEXT NOT NULL CHECK (decision IN ('recommend','reject','revise')),
    summary TEXT NOT NULL,
    policy_ref TEXT NOT NULL,
    published_by TEXT REFERENCES users(id),
    published_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(application_id, version)
);

CREATE UNIQUE INDEX idx_conclusions_current
    ON conclusions(application_id)
    WHERE status = 'published';

CREATE TABLE regional_adoptions (
    id TEXT PRIMARY KEY,
    conclusion_id TEXT NOT NULL REFERENCES conclusions(id),
    region TEXT NOT NULL,
    institution_id TEXT NOT NULL REFERENCES institutions(id),
    status TEXT NOT NULL CHECK (status IN ('active','revoked')),
    policy_ref TEXT NOT NULL,
    adopted_by TEXT NOT NULL REFERENCES users(id),
    adopted_at TEXT NOT NULL,
    revoked_by TEXT REFERENCES users(id),
    revoked_at TEXT,
    revoke_reason TEXT NOT NULL DEFAULT '',
    UNIQUE(conclusion_id, region, institution_id)
);

CREATE INDEX idx_regional_adoptions_region_status ON regional_adoptions(region, status);

CREATE TABLE worker_jobs (
    id TEXT PRIMARY KEY,
    job_type TEXT NOT NULL CHECK (job_type IN ('observation_reminder','anomaly_review','season_summary','adoption_follow_up')),
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL CHECK (status IN ('pending','running','completed','failed','dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL CHECK (max_attempts > 0),
    available_at TEXT NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_expires_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(job_type, object_type, object_id, status)
);

CREATE INDEX idx_worker_jobs_claim ON worker_jobs(status, available_at, lease_expires_at);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_user_id TEXT NOT NULL,
    institution_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    outcome TEXT NOT NULL,
    policy_ref TEXT NOT NULL DEFAULT '',
    before_json TEXT NOT NULL DEFAULT '',
    after_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_audit_object_time ON audit_events(object_type, object_id, created_at);
CREATE INDEX idx_audit_actor_request ON audit_events(actor_user_id, request_id);

CREATE TABLE idempotency_keys (
    institution_id TEXT NOT NULL REFERENCES institutions(id),
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    idem_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_code INTEGER NOT NULL,
    response_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (institution_id, method, path, idem_key)
);

CREATE INDEX idx_idempotency_expiry ON idempotency_keys(expires_at);
