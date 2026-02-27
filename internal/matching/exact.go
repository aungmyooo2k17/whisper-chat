package matching

import (
	"context"
)

// MatchCandidate represents a successful match between two users.
type MatchCandidate struct {
	SessionA        string
	SessionB        string
	SharedInterests []string
}

// TryExactMatch attempts Tier 1 matching: find a user with an identical
// interest set (same hash). Returns nil if no exact match is available.
func (q *Queue) TryExactMatch(ctx context.Context, sessionID string) (*MatchCandidate, error) {
	entry, err := q.GetEntry(ctx, sessionID)
	if err != nil || entry == nil {
		return nil, err
	}

	candidates, err := q.GetExactCandidates(ctx, entry.Hash)
	if err != nil {
		return nil, err
	}

	for _, candidateID := range candidates {
		if candidateID == sessionID {
			continue
		}

		// Validate candidate is still in the global queue.
		queued, err := q.IsQueued(ctx, candidateID)
		if err != nil {
			continue
		}
		if !queued {
			continue // stale entry, cleanup will remove it
		}

		return &MatchCandidate{
			SessionA:        sessionID,
			SessionB:        candidateID,
			SharedInterests: entry.Interests, // all interests match (exact)
		}, nil
	}

	return nil, nil
}
