package matching

import (
	"context"
)

// TryRandomMatch attempts Tier 4 matching: pair with any other queued user
// regardless of interests. The queue is ordered by join time (oldest first),
// so picking the first non-self entry is fair. Returns nil if no other user
// is queued.
func (q *Queue) TryRandomMatch(ctx context.Context, sessionID string) (*MatchCandidate, error) {
	allQueued, err := q.GetAllQueued(ctx)
	if err != nil {
		return nil, err
	}

	for _, candidateID := range allQueued {
		if candidateID == sessionID {
			continue
		}

		// Validate candidate is still in the queue.
		queued, err := q.IsQueued(ctx, candidateID)
		if err != nil || !queued {
			continue
		}

		return &MatchCandidate{
			SessionA:        sessionID,
			SessionB:        candidateID,
			SharedInterests: nil, // no shared interests (random pairing)
		}, nil
	}

	return nil, nil
}
