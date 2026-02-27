package matching

import (
	"context"
	"sort"
)

// TryOverlapMatch attempts Tier 2 matching: scan per-interest sets and find
// the candidate with the highest number of overlapping interests. Returns nil
// if no overlap candidate is available.
func (q *Queue) TryOverlapMatch(ctx context.Context, sessionID string) (*MatchCandidate, error) {
	entry, err := q.GetEntry(ctx, sessionID)
	if err != nil || entry == nil {
		return nil, err
	}

	// Count how many interests each candidate shares with us.
	scores := make(map[string]int)
	candidateInterests := make(map[string]map[string]bool)

	for _, tag := range entry.Interests {
		members, err := q.GetInterestCandidates(ctx, tag)
		if err != nil {
			continue
		}
		for _, memberID := range members {
			if memberID == sessionID {
				continue
			}
			scores[memberID]++
			if candidateInterests[memberID] == nil {
				candidateInterests[memberID] = make(map[string]bool)
			}
			candidateInterests[memberID][tag] = true
		}
	}

	if len(scores) == 0 {
		return nil, nil
	}

	// Rank candidates by overlap count (descending).
	type scored struct {
		id    string
		count int
	}
	ranked := make([]scored, 0, len(scores))
	for id, count := range scores {
		ranked = append(ranked, scored{id, count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].count > ranked[j].count
	})

	// Return the first valid candidate.
	for _, candidate := range ranked {
		queued, err := q.IsQueued(ctx, candidate.id)
		if err != nil || !queued {
			continue
		}

		shared := make([]string, 0, candidate.count)
		for tag := range candidateInterests[candidate.id] {
			shared = append(shared, tag)
		}
		sort.Strings(shared)

		return &MatchCandidate{
			SessionA:        sessionID,
			SessionB:        candidate.id,
			SharedInterests: shared,
		}, nil
	}

	return nil, nil
}
