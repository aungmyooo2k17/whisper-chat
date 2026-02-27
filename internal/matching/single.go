package matching

import (
	"context"
	"sort"
)

// TrySingleInterestMatch attempts Tier 3 matching: find ANY queued user who
// shares at least one interest. Unlike Tier 2 (which picks the best overlap),
// this accepts the first candidate with any overlap >= 1. Returns nil if no
// candidate is available.
func (q *Queue) TrySingleInterestMatch(ctx context.Context, sessionID string) (*MatchCandidate, error) {
	entry, err := q.GetEntry(ctx, sessionID)
	if err != nil || entry == nil {
		return nil, err
	}

	// Collect all candidates who share at least one interest, and track
	// which interests they share.
	seen := make(map[string]bool)
	candidateInterests := make(map[string][]string)

	for _, tag := range entry.Interests {
		members, err := q.GetInterestCandidates(ctx, tag)
		if err != nil {
			continue
		}
		for _, memberID := range members {
			if memberID == sessionID {
				continue
			}
			if !seen[memberID] {
				seen[memberID] = true
				candidateInterests[memberID] = []string{tag}
			} else {
				candidateInterests[memberID] = append(candidateInterests[memberID], tag)
			}
		}
	}

	// Return the first valid candidate (any overlap >= 1).
	for candidateID, shared := range candidateInterests {
		queued, err := q.IsQueued(ctx, candidateID)
		if err != nil || !queued {
			continue
		}

		sort.Strings(shared)

		return &MatchCandidate{
			SessionA:        sessionID,
			SessionB:        candidateID,
			SharedInterests: shared,
		}, nil
	}

	return nil, nil
}
