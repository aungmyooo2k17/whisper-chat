package ban

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newTestStore creates a Store connected to a local Redis instance and flushes
// all ban and report keys before returning.  Tests that call this helper
// require a running Redis on localhost:6379.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	// Clean up any leftover test keys (both ban: and reports: prefixes).
	for _, prefix := range []string{BanPrefix + "test_*", ReportsPrefix + "test_*"} {
		iter := client.Scan(ctx, 0, prefix, 100).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
	}
	t.Cleanup(func() {
		for _, prefix := range []string{BanPrefix + "test_*", ReportsPrefix + "test_*"} {
			iter := client.Scan(ctx, 0, prefix, 100).Iterator()
			for iter.Next(ctx) {
				client.Del(ctx, iter.Val())
			}
		}
		client.Close()
	})
	return NewStore(client)
}

func TestIsBanned_NotBanned(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	banned, remaining, reason, err := store.IsBanned(ctx, "test_no_ban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if banned {
		t.Errorf("expected not banned, got banned (remaining=%d reason=%q)", remaining, reason)
	}
}

func TestBanAndCheck(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_ban_check"

	if err := store.Ban(ctx, fp, 30*time.Second, "spam"); err != nil {
		t.Fatalf("Ban() error: %v", err)
	}

	banned, remaining, reason, err := store.IsBanned(ctx, fp)
	if err != nil {
		t.Fatalf("IsBanned() error: %v", err)
	}
	if !banned {
		t.Fatal("expected banned=true")
	}
	if reason != "spam" {
		t.Errorf("expected reason=%q, got %q", "spam", reason)
	}
	if remaining <= 0 || remaining > 30 {
		t.Errorf("expected remaining in (0,30], got %d", remaining)
	}
}

func TestUnban(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_unban"

	if err := store.Ban(ctx, fp, time.Minute, "test"); err != nil {
		t.Fatalf("Ban() error: %v", err)
	}

	// Verify banned.
	banned, _, _, _ := store.IsBanned(ctx, fp)
	if !banned {
		t.Fatal("expected banned=true after Ban()")
	}

	// Unban and verify.
	if err := store.Unban(ctx, fp); err != nil {
		t.Fatalf("Unban() error: %v", err)
	}
	banned, _, _, err := store.IsBanned(ctx, fp)
	if err != nil {
		t.Fatalf("IsBanned() error: %v", err)
	}
	if banned {
		t.Error("expected not banned after Unban()")
	}
}

// ---------------------------------------------------------------------------
// Escalation tests (ABUSE-6)
// ---------------------------------------------------------------------------

func TestEscalationDuration(t *testing.T) {
	cases := []struct {
		count    int
		expected time.Duration
	}{
		{0, Ban15Min},
		{1, Ban15Min},
		{2, Ban1Hour},
		{3, Ban24Hour},
		{4, Ban24Hour},
		{10, Ban24Hour},
	}
	for _, tc := range cases {
		got := escalationDuration(tc.count)
		if got != tc.expected {
			t.Errorf("escalationDuration(%d) = %v, want %v", tc.count, got, tc.expected)
		}
	}
}

func TestGetOffenseCount_NoOffenses(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	count, err := store.GetOffenseCount(ctx, "test_no_offenses")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 offenses, got %d", count)
	}
}

func TestEscalate_FirstOffense_15Min(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_escalate_1st"

	duration, err := store.Escalate(ctx, fp, "spam")
	if err != nil {
		t.Fatalf("Escalate() error: %v", err)
	}
	if duration != Ban15Min {
		t.Errorf("1st offense: expected %v, got %v", Ban15Min, duration)
	}

	// Verify the ban is in place.
	banned, remaining, reason, err := store.IsBanned(ctx, fp)
	if err != nil {
		t.Fatalf("IsBanned() error: %v", err)
	}
	if !banned {
		t.Fatal("expected banned=true after 1st offense")
	}
	if reason != "spam" {
		t.Errorf("expected reason=%q, got %q", "spam", reason)
	}
	// 15 min = 900 seconds; allow some slack for test execution time.
	if remaining < 890 || remaining > 900 {
		t.Errorf("expected remaining ~900s, got %d", remaining)
	}

	// Offense counter should be 1.
	count, err := store.GetOffenseCount(ctx, fp)
	if err != nil {
		t.Fatalf("GetOffenseCount() error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected offense count=1, got %d", count)
	}
}

func TestEscalate_SecondOffense_1Hour(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_escalate_2nd"

	// First offense.
	if _, err := store.Escalate(ctx, fp, "spam"); err != nil {
		t.Fatalf("1st Escalate() error: %v", err)
	}

	// Unban so we can clearly test the second offense ban duration.
	store.Unban(ctx, fp)

	// Second offense.
	duration, err := store.Escalate(ctx, fp, "harassment")
	if err != nil {
		t.Fatalf("2nd Escalate() error: %v", err)
	}
	if duration != Ban1Hour {
		t.Errorf("2nd offense: expected %v, got %v", Ban1Hour, duration)
	}

	// Verify ban.
	banned, remaining, _, err := store.IsBanned(ctx, fp)
	if err != nil {
		t.Fatalf("IsBanned() error: %v", err)
	}
	if !banned {
		t.Fatal("expected banned=true after 2nd offense")
	}
	// 1 hour = 3600 seconds.
	if remaining < 3590 || remaining > 3600 {
		t.Errorf("expected remaining ~3600s, got %d", remaining)
	}

	// Offense counter should be 2.
	count, _ := store.GetOffenseCount(ctx, fp)
	if count != 2 {
		t.Errorf("expected offense count=2, got %d", count)
	}
}

func TestEscalate_ThirdOffense_24Hour(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_escalate_3rd"

	// First and second offenses.
	store.Escalate(ctx, fp, "spam")
	store.Escalate(ctx, fp, "spam")
	store.Unban(ctx, fp)

	// Third offense.
	duration, err := store.Escalate(ctx, fp, "serious")
	if err != nil {
		t.Fatalf("3rd Escalate() error: %v", err)
	}
	if duration != Ban24Hour {
		t.Errorf("3rd offense: expected %v, got %v", Ban24Hour, duration)
	}

	// 24h = 86400 seconds.
	_, remaining, _, _ := store.IsBanned(ctx, fp)
	if remaining < 86390 || remaining > 86400 {
		t.Errorf("expected remaining ~86400s, got %d", remaining)
	}
}

func TestEscalate_FourthOffense_StillCapped24Hour(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_escalate_4th"

	// Build up 3 offenses.
	store.Escalate(ctx, fp, "spam")
	store.Escalate(ctx, fp, "spam")
	store.Escalate(ctx, fp, "spam")
	store.Unban(ctx, fp)

	// Fourth offense — should still be 24h (no permanent bans).
	duration, err := store.Escalate(ctx, fp, "repeat")
	if err != nil {
		t.Fatalf("4th Escalate() error: %v", err)
	}
	if duration != Ban24Hour {
		t.Errorf("4th offense: expected %v (capped), got %v", Ban24Hour, duration)
	}
}

func TestReportAndCheck_BelowThreshold(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_report_below"

	// First report — below threshold.
	banned, duration, err := store.ReportAndCheck(ctx, fp, "rude")
	if err != nil {
		t.Fatalf("ReportAndCheck() error: %v", err)
	}
	if banned {
		t.Error("expected banned=false after 1 report")
	}
	if duration != 0 {
		t.Errorf("expected duration=0, got %v", duration)
	}

	// Second report — still below.
	banned, _, err = store.ReportAndCheck(ctx, fp, "rude")
	if err != nil {
		t.Fatalf("ReportAndCheck() error: %v", err)
	}
	if banned {
		t.Error("expected banned=false after 2 reports")
	}

	// Should not be banned yet.
	isBanned, _, _, _ := store.IsBanned(ctx, fp)
	if isBanned {
		t.Error("user should not be banned with only 2 reports")
	}
}

func TestReportAndCheck_AutoBanAt3Reports(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_report_autoban"

	// 1st and 2nd reports — no ban.
	store.ReportAndCheck(ctx, fp, "spam")
	store.ReportAndCheck(ctx, fp, "spam")

	// 3rd report — should trigger auto-ban.
	banned, duration, err := store.ReportAndCheck(ctx, fp, "spam")
	if err != nil {
		t.Fatalf("ReportAndCheck() error: %v", err)
	}
	if !banned {
		t.Fatal("expected banned=true after 3 reports")
	}
	// 3rd report count = 3, which maps to Ban24Hour via escalationDuration.
	if duration != Ban24Hour {
		t.Errorf("expected ban duration %v, got %v", Ban24Hour, duration)
	}

	// Verify the ban is in Redis with reason "multiple_reports".
	isBanned, _, reason, _ := store.IsBanned(ctx, fp)
	if !isBanned {
		t.Fatal("expected IsBanned=true after auto-ban")
	}
	if reason != "multiple_reports" {
		t.Errorf("expected reason=%q, got %q", "multiple_reports", reason)
	}
}

func TestReportAndCheck_SubsequentReportsStillBan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_report_subsequent"

	// Accumulate 3 reports to trigger auto-ban.
	store.ReportAndCheck(ctx, fp, "spam")
	store.ReportAndCheck(ctx, fp, "spam")
	store.ReportAndCheck(ctx, fp, "spam")

	// 4th report — should still return banned=true.
	banned, duration, err := store.ReportAndCheck(ctx, fp, "spam")
	if err != nil {
		t.Fatalf("ReportAndCheck() error: %v", err)
	}
	if !banned {
		t.Fatal("expected banned=true for 4th+ report")
	}
	// count=4 maps to Ban24Hour (capped).
	if duration != Ban24Hour {
		t.Errorf("expected %v, got %v", Ban24Hour, duration)
	}
}

func TestReportCounterTTL(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_report_ttl"

	// File a report to create the counter.
	store.ReportAndCheck(ctx, fp, "test")

	// Verify the counter has a TTL set (should be close to 24h).
	key := ReportsPrefix + fp
	ttl, err := store.client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL() error: %v", err)
	}
	// TTL should be positive and close to 24h (86400s). Allow 10s slack.
	if ttl < 86390*time.Second || ttl > 86400*time.Second {
		t.Errorf("expected TTL ~24h, got %v", ttl)
	}
}

func TestGetOffenseCount_AfterEscalate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fp := "test_offense_count"

	store.Escalate(ctx, fp, "a")
	store.Escalate(ctx, fp, "b")
	store.Escalate(ctx, fp, "c")

	count, err := store.GetOffenseCount(ctx, fp)
	if err != nil {
		t.Fatalf("GetOffenseCount() error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}
}
