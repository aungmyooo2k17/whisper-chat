-- 001_create_abuse_reports.up.sql
-- Creates the abuse_reports table for storing user reports.
-- Reports are retained for 30 days and used for auto-ban logic
-- (3 reports against the same fingerprint in 24h -> 1h ban).

CREATE TABLE IF NOT EXISTS abuse_reports (
    id                    BIGSERIAL    PRIMARY KEY,
    reporter_fingerprint  TEXT         NOT NULL,
    reported_fingerprint  TEXT         NOT NULL,
    chat_id               TEXT         NOT NULL,
    reason                TEXT         NOT NULL CHECK (reason IN ('harassment', 'spam', 'explicit', 'other')),
    messages              JSONB,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Index for auto-ban lookup: 3 reports against same fingerprint in 24h.
CREATE INDEX idx_abuse_reports_reported_fingerprint_created
    ON abuse_reports (reported_fingerprint, created_at);

-- Index for 30-day retention cleanup job.
CREATE INDEX idx_abuse_reports_created_at
    ON abuse_reports (created_at);
