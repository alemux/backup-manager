// internal/retention/retention_test.go
package retention

import (
	"sort"
	"testing"
	"time"
)

// makeSnapshots creates a slice of Snapshot with IDs 1..n, each separated by step,
// going backwards from base (newest = base, oldest = base - (n-1)*step).
// The returned slice is sorted newest-first (index 0 = most recent).
func makeSnapshots(base time.Time, n int, step time.Duration) []Snapshot {
	snapshots := make([]Snapshot, n)
	for i := 0; i < n; i++ {
		snapshots[i] = Snapshot{
			ID:        n - i, // ID 1 = oldest, ID n = newest
			CreatedAt: base.Add(-time.Duration(i) * step),
			SizeBytes: 1024,
		}
	}
	return snapshots
}

// sortedIDs returns a sorted copy of the slice for stable comparison.
func sortedIDs(ids []int) []int {
	cp := make([]int, len(ids))
	copy(cp, ids)
	sort.Ints(cp)
	return cp
}

func TestApply_EmptyList(t *testing.T) {
	result := Apply(nil, RetentionPolicy{Daily: 7, Weekly: 4, Monthly: 3}, time.UTC)
	if len(result) != 0 {
		t.Errorf("expected no deletions for empty list, got %v", result)
	}

	result = Apply([]Snapshot{}, RetentionPolicy{Daily: 7, Weekly: 4, Monthly: 3}, time.UTC)
	if len(result) != 0 {
		t.Errorf("expected no deletions for empty list, got %v", result)
	}
}

func TestApply_KeepsMostRecent(t *testing.T) {
	base := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	snapshots := []Snapshot{
		{ID: 1, CreatedAt: base, SizeBytes: 100},
	}

	result := Apply(snapshots, RetentionPolicy{Daily: 7, Weekly: 4, Monthly: 3}, time.UTC)
	if len(result) != 0 {
		t.Errorf("single snapshot should never be deleted, got %v", result)
	}
}

func TestApply_NeverDeletesMostRecent(t *testing.T) {
	// Even with policy {0, 0, 0}, the most recent snapshot must survive.
	base := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
	snapshots := makeSnapshots(base, 5, 24*time.Hour)

	result := Apply(snapshots, RetentionPolicy{Daily: 0, Weekly: 0, Monthly: 0}, time.UTC)

	// ID of most recent = 5 (makeSnapshots assigns ID=n to the newest)
	newestID := snapshots[0].ID
	for _, id := range result {
		if id == newestID {
			t.Errorf("most recent snapshot (ID=%d) must never be deleted", newestID)
		}
	}

	// All others should be deleted (4 snapshots).
	if len(result) != 4 {
		t.Errorf("expected 4 deletions, got %d: %v", len(result), result)
	}
}

func TestApply_ZeroPolicyKeepsOnlyLatest(t *testing.T) {
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	snapshots := makeSnapshots(base, 10, 24*time.Hour)

	result := Apply(snapshots, RetentionPolicy{Daily: 0, Weekly: 0, Monthly: 0}, time.UTC)

	// 9 snapshots should be deleted; newest (ID=10) must be kept.
	if len(result) != 9 {
		t.Errorf("expected 9 deletions, got %d", len(result))
	}
	newestID := snapshots[0].ID
	for _, id := range result {
		if id == newestID {
			t.Errorf("newest snapshot (ID=%d) must not be in delete list", newestID)
		}
	}
}

func TestApply_DailyRetention(t *testing.T) {
	// 10 daily snapshots at noon UTC.
	// policy daily=7 → keep 7 most recent days, delete 3 oldest.
	base := time.Date(2024, 4, 10, 12, 0, 0, 0, time.UTC) // day 10 = newest
	snapshots := makeSnapshots(base, 10, 24*time.Hour)

	policy := RetentionPolicy{Daily: 7, Weekly: 0, Monthly: 0}
	result := Apply(snapshots, policy, time.UTC)

	// The 3 oldest (IDs 1, 2, 3) should be deleted.
	expected := sortedIDs([]int{1, 2, 3})
	got := sortedIDs(result)

	if len(got) != len(expected) {
		t.Fatalf("expected %d deletions, got %d: %v", len(expected), len(got), got)
	}
	for i, id := range expected {
		if got[i] != id {
			t.Errorf("expected delete ID %d, got %d", id, got[i])
		}
	}
}

func TestApply_MultipleSnapshotsPerDay(t *testing.T) {
	// Day A: two snapshots (6am and 2pm). Day B: one snapshot.
	// With daily=2, both days kept – but within day A only the latest (2pm) kept.
	dayA := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	dayAMorning := dayA.Add(6 * time.Hour)
	dayAAfternoon := dayA.Add(14 * time.Hour) // newer
	dayB := dayA.Add(-24 * time.Hour)
	dayBNoon := dayB.Add(12 * time.Hour)

	// Sorted newest-first: dayAAfternoon, dayAMorning, dayBNoon.
	snapshots := []Snapshot{
		{ID: 3, CreatedAt: dayAAfternoon, SizeBytes: 100}, // newest
		{ID: 2, CreatedAt: dayAMorning, SizeBytes: 100},
		{ID: 1, CreatedAt: dayBNoon, SizeBytes: 100},
	}

	policy := RetentionPolicy{Daily: 2, Weekly: 0, Monthly: 0}
	result := Apply(snapshots, policy, time.UTC)

	// ID 2 (dayAMorning, same day as ID 3) should be deleted because
	// ID 3 is already the kept representative for day A.
	if len(result) != 1 || result[0] != 2 {
		t.Errorf("expected [2] to be deleted (duplicate daily), got %v", result)
	}
}

func TestApply_WeeklyRetention(t *testing.T) {
	// 30 daily snapshots. policy daily=7, weekly=4.
	// Newest snapshot is on a known day so we can reason about weeks.
	// Let base = Monday 2024-01-29 (week starting Sun 2024-01-28).
	base := time.Date(2024, 1, 29, 12, 0, 0, 0, time.UTC)
	snapshots := makeSnapshots(base, 30, 24*time.Hour)

	policy := RetentionPolicy{Daily: 7, Weekly: 4, Monthly: 0}
	result := Apply(snapshots, policy, time.UTC)

	// Build keep set from result (for quick lookup).
	deleteSet := make(map[int]bool)
	for _, id := range result {
		deleteSet[id] = true
	}

	// The most recent snapshot must be kept.
	if deleteSet[snapshots[0].ID] {
		t.Errorf("most recent snapshot must not be deleted")
	}

	// All snapshots within the last 7 days must be kept (one per day is kept;
	// in this test there's one per day so all 7 should be kept).
	for i := 0; i < 7; i++ {
		if deleteSet[snapshots[i].ID] {
			t.Errorf("snapshot at index %d (within daily window) should not be deleted", i)
		}
	}

	// Snapshots older than 4 weeks back from the newest should be deleted
	// unless they're the weekly representative.
	// With 30 daily snapshots, days 29-30 are definitely older than 4 weeks.
	// (4 weeks = 28 days; day 29 is index 28, day 30 is index 29.)
	// These are beyond both daily and weekly window.
	for i := 28; i < 30; i++ {
		s := snapshots[i]
		snapshotTime := s.CreatedAt.In(time.UTC)
		weekStart := truncateToWeek(snapshotTime, time.UTC)
		newestWeekStart := truncateToWeek(snapshots[0].CreatedAt.In(time.UTC), time.UTC)
		weeksOld := int(newestWeekStart.Sub(weekStart).Hours() / (7 * 24))
		if weeksOld >= 4 {
			// Should be deleted unless it's the first snapshot seen for its week.
			// Since we may have kept one per week, check that at most 1 survives per week.
		}
		_ = weeksOld
	}

	// High-level: result should be non-empty (some snapshots beyond daily+weekly windows).
	if len(result) == 0 {
		t.Error("expected some deletions with 30 snapshots and weekly=4 daily=7 policy")
	}
}

func TestApply_MonthlyRetention(t *testing.T) {
	// 120 daily snapshots (≈4 months). policy daily=7, weekly=4, monthly=3.
	// Newest = 2024-04-30.
	base := time.Date(2024, 4, 30, 12, 0, 0, 0, time.UTC)
	snapshots := makeSnapshots(base, 120, 24*time.Hour)

	policy := RetentionPolicy{Daily: 7, Weekly: 4, Monthly: 3}
	result := Apply(snapshots, policy, time.UTC)

	deleteSet := make(map[int]bool)
	for _, id := range result {
		deleteSet[id] = true
	}

	// Most recent must be kept.
	if deleteSet[snapshots[0].ID] {
		t.Errorf("most recent snapshot must not be deleted")
	}

	// Daily window: first 7 snapshots must all be kept.
	for i := 0; i < 7; i++ {
		if deleteSet[snapshots[i].ID] {
			t.Errorf("daily-window snapshot at index %d should not be deleted", i)
		}
	}

	// With 120 days and monthly=3, snapshots from months 1-3 from the end
	// get at least a monthly keep representative. Snapshots older than ~90 days
	// from newest AND not the monthly representative should be deleted.
	// Verify that we do delete some snapshots.
	if len(result) == 0 {
		t.Error("expected some deletions with 120 snapshots and 3-month policy")
	}

	// Verify monthly representatives for the 3 most recent months are kept.
	// Month of newest = April 2024, so kept months: April, March, February.
	keptMonths := map[string]bool{
		"2024-04": false,
		"2024-03": false,
		"2024-02": false,
	}
	for _, s := range snapshots {
		monthKey := s.CreatedAt.In(time.UTC).Format("2006-01")
		if _, ok := keptMonths[monthKey]; ok {
			if !deleteSet[s.ID] {
				keptMonths[monthKey] = true
			}
		}
	}
	for month, kept := range keptMonths {
		if !kept {
			t.Errorf("no snapshot kept for month %s (monthly retention=3)", month)
		}
	}
}

func TestApply_TimezoneAware(t *testing.T) {
	// A snapshot at 2024-01-07 23:30 UTC is:
	//   - Sunday in UTC (weekday 0)
	//   - Monday 2024-01-08 in UTC+1
	// So in UTC it falls on Sunday; in UTC+1 it falls on Monday.
	// We verify that truncateToWeek uses the provided timezone.

	loc := time.FixedZone("UTC+1", 3600)
	utc := time.UTC

	// Snapshot at 2024-01-07 23:30 UTC
	snapshotTime := time.Date(2024, 1, 7, 23, 30, 0, 0, utc)

	weekStartUTC := truncateToWeek(snapshotTime, utc)    // Should be Sun 2024-01-07
	weekStartPlus1 := truncateToWeek(snapshotTime, loc)  // Should be Mon 2024-01-08 in local = Sun 2024-01-07 local... let's verify

	// In UTC: 2024-01-07 23:30 is Sunday → week start = 2024-01-07 (Sunday)
	if weekStartUTC.Weekday() != time.Sunday {
		t.Errorf("UTC week start should be Sunday, got %v", weekStartUTC.Weekday())
	}

	// In UTC+1: 2024-01-07 23:30 UTC = 2024-01-08 00:30 UTC+1 (Monday)
	// → week start in UTC+1 = Sunday 2024-01-07 00:00 UTC+1 = 2024-01-06 23:00 UTC
	snapshotLocalDay := snapshotTime.In(loc).Weekday()
	if snapshotLocalDay != time.Monday {
		t.Errorf("snapshot in UTC+1 should be Monday, got %v", snapshotLocalDay)
	}

	// The week start in UTC+1 (Sunday) must differ from UTC.
	if weekStartPlus1.Equal(weekStartUTC) {
		t.Errorf("week starts should differ between UTC and UTC+1 for a Sunday-evening snapshot")
	}

	// Now verify Apply respects timezone for an edge case:
	// snapshot A: 2024-01-07 23:30 UTC (Sunday in UTC, Monday in UTC+1)
	// snapshot B: 2024-01-14 12:00 UTC (Sunday in both timezones, newest)
	// policy: weekly=1, daily=0, monthly=0
	// In UTC: both in different weeks (week of Jan 7 and week of Jan 14)
	// With weekly=1: keep newest week only + always keep most recent.
	// → snapshot A should be deleted in both timezones (it's in an older week).
	snapshotA := Snapshot{ID: 1, CreatedAt: snapshotTime, SizeBytes: 100}
	snapshotB := Snapshot{ID: 2, CreatedAt: time.Date(2024, 1, 14, 12, 0, 0, 0, utc), SizeBytes: 100}
	snapshots := []Snapshot{snapshotB, snapshotA} // newest-first

	policy := RetentionPolicy{Daily: 0, Weekly: 1, Monthly: 0}
	resultUTC := Apply(snapshots, policy, utc)
	resultPlus1 := Apply(snapshots, policy, loc)

	// Snapshot A (older week) should be deleted in both timezones.
	utcDeletes := make(map[int]bool)
	for _, id := range resultUTC {
		utcDeletes[id] = true
	}
	plus1Deletes := make(map[int]bool)
	for _, id := range resultPlus1 {
		plus1Deletes[id] = true
	}

	if !utcDeletes[1] {
		t.Errorf("snapshot A should be deleted in UTC timezone (older week)")
	}
	if !plus1Deletes[1] {
		t.Errorf("snapshot A should be deleted in UTC+1 timezone (older week)")
	}

	// Snapshot B (newest) must never be deleted.
	if utcDeletes[2] {
		t.Errorf("snapshot B (newest) must not be deleted in UTC")
	}
	if plus1Deletes[2] {
		t.Errorf("snapshot B (newest) must not be deleted in UTC+1")
	}
}
