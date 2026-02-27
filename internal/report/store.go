// Package report provides PostgreSQL-backed storage for abuse reports.
// Each report captures who reported whom, the chat context, and the last
// few messages exchanged (for moderator review).
package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// validReasons is the set of allowed reason values, matching the CHECK
// constraint on the abuse_reports table.
var validReasons = map[string]bool{
	"harassment": true,
	"spam":       true,
	"explicit":   true,
	"other":      true,
}

// Store manages abuse reports in PostgreSQL.
type Store struct {
	db *sql.DB
}

// Report represents a single abuse report to be persisted.
type Report struct {
	ReporterFingerprint string
	ReportedFingerprint string
	ChatID              string
	Reason              string
	Messages            []MessageEntry // last N messages from the chat buffer
}

// MessageEntry is one message in the conversation snapshot attached to a report.
type MessageEntry struct {
	From string `json:"from"` // "user_a" or "user_b" (anonymised)
	Text string `json:"text"`
	Ts   int64  `json:"ts"`
}

// NewStore creates a new report store backed by the given database handle.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create inserts an abuse report into PostgreSQL.
// Messages are marshalled to JSONB. The reason is validated against the
// allowed set before insertion.
func (s *Store) Create(ctx context.Context, report *Report) error {
	if !validReasons[report.Reason] {
		return fmt.Errorf("report: invalid reason %q", report.Reason)
	}

	var messagesJSON []byte
	if len(report.Messages) > 0 {
		var err error
		messagesJSON, err = json.Marshal(report.Messages)
		if err != nil {
			return fmt.Errorf("report: marshal messages: %w", err)
		}
	}

	const query = `
		INSERT INTO abuse_reports (reporter_fingerprint, reported_fingerprint, chat_id, reason, messages)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := s.db.ExecContext(ctx, query,
		report.ReporterFingerprint,
		report.ReportedFingerprint,
		report.ChatID,
		report.Reason,
		messagesJSON,
	)
	if err != nil {
		return fmt.Errorf("report: insert: %w", err)
	}
	return nil
}

// CountRecent returns the number of reports filed against a fingerprint
// within the given time window. This is useful for auto-ban logic
// (e.g. 3 reports in 24 hours triggers a ban).
func (s *Store) CountRecent(ctx context.Context, reportedFingerprint string, window time.Duration) (int, error) {
	const query = `
		SELECT COUNT(*)
		FROM abuse_reports
		WHERE reported_fingerprint = $1
		  AND created_at >= NOW() - $2::interval`

	var count int
	err := s.db.QueryRowContext(ctx, query, reportedFingerprint, window.String()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("report: count recent: %w", err)
	}
	return count, nil
}
