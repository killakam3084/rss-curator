package storage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
)

// ── Suggestions ──────────────────────────────────────────────────────────────

func TestUpsertAndListSuggestions(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	rows := []SuggestionRow{
		{ShowName: "Succession", ContentType: "show", Reason: "drama", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
		{ShowName: "Dune Part Two", ContentType: "movie", Reason: "sci-fi", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
	}
	if err := store.UpsertSuggestions(rows); err != nil {
		t.Fatalf("UpsertSuggestions: %v", err)
	}

	list, err := store.ListSuggestions()
	if err != nil {
		t.Fatalf("ListSuggestions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	for _, r := range list {
		if r.Status != "active" {
			t.Errorf("expected status 'active', got %q", r.Status)
		}
	}
}

func TestUpsertSuggestions_Idempotent(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	row := SuggestionRow{ShowName: "The Bear", ContentType: "show", Reason: "comedy", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()}
	_ = store.UpsertSuggestions([]SuggestionRow{row})
	_ = store.UpsertSuggestions([]SuggestionRow{row}) // duplicate — INSERT OR IGNORE

	list, _ := store.ListSuggestions()
	if len(list) != 1 {
		t.Errorf("expected 1 after idempotent upsert, got %d", len(list))
	}
}

func TestDismissSuggestion_Permanent(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	_ = store.UpsertSuggestions([]SuggestionRow{
		{ShowName: "Severance", ContentType: "show", Reason: "thriller", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
	})
	if err := store.DismissSuggestion("Severance", time.Time{}); err != nil {
		t.Fatalf("DismissSuggestion: %v", err)
	}

	list, _ := store.ListSuggestions()
	if len(list) != 0 {
		t.Errorf("dismissed suggestion should not appear in active list")
	}
}

func TestDismissSuggestion_Temporal(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	_ = store.UpsertSuggestions([]SuggestionRow{
		{ShowName: "Andor", ContentType: "show", Reason: "sci-fi", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
	})
	until := time.Now().Add(90 * 24 * time.Hour)
	if err := store.DismissSuggestion("Andor", until); err != nil {
		t.Fatalf("DismissSuggestion temporal: %v", err)
	}
	list, _ := store.ListSuggestions()
	if len(list) != 0 {
		t.Errorf("temporally dismissed suggestion should not appear in active list")
	}
}

func TestReactivateExpiredDismissals(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	_ = store.UpsertSuggestions([]SuggestionRow{
		{ShowName: "Peaky Blinders", ContentType: "show", Reason: "crime", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
	})
	// Dismiss with an already-expired time (1 hour in the past).
	expired := time.Now().Add(-time.Hour)
	if err := store.DismissSuggestion("Peaky Blinders", expired); err != nil {
		t.Fatalf("DismissSuggestion: %v", err)
	}
	// Confirm it's dismissed.
	list, _ := store.ListSuggestions()
	if len(list) != 0 {
		t.Fatal("should be dismissed")
	}

	n, err := store.ReactivateExpiredDismissals()
	if err != nil {
		t.Fatalf("ReactivateExpiredDismissals: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 reactivated, got %d", n)
	}
	list, _ = store.ListSuggestions()
	if len(list) != 1 {
		t.Errorf("expected 1 active after reactivation, got %d", len(list))
	}
}

func TestPruneSuggestions(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	_ = store.UpsertSuggestions([]SuggestionRow{
		{ShowName: "House of Cards", ContentType: "show", Reason: "political", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
		{ShowName: "Stranger Things", ContentType: "show", Reason: "horror", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
	})

	// Prune "house of cards" (case-insensitive)
	pruned, err := store.PruneSuggestions([]string{"house of cards"})
	if err != nil {
		t.Fatalf("PruneSuggestions: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	list, _ := store.ListSuggestions()
	if len(list) != 1 || list[0].ShowName != "Stranger Things" {
		t.Errorf("unexpected remaining suggestions: %+v", list)
	}
}

func TestPruneSuggestions_Empty(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	pruned, err := store.PruneSuggestions(nil)
	if err != nil {
		t.Fatalf("PruneSuggestions(nil): %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0, got %d", pruned)
	}
}

func TestSuggestionCount(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	n, err := store.SuggestionCount()
	if err != nil {
		t.Fatalf("SuggestionCount: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}

	_ = store.UpsertSuggestions([]SuggestionRow{
		{ShowName: "Show A", ContentType: "show", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
		{ShowName: "Show B", ContentType: "show", RuleJSON: json.RawMessage(`{}`), GeneratedAt: time.Now()},
	})

	n, err = store.SuggestionCount()
	if err != nil {
		t.Fatalf("SuggestionCount after insert: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

// ── Raw feed items ────────────────────────────────────────────────────────────

func TestAddAndGetRawFeedItems(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	now := time.Now()
	item := models.RawFeedItem{
		FeedItem: models.FeedItem{
			Title:   "My.Show.S01E01.1080p.WEB-DL",
			Link:    "http://example.com/show.torrent",
			GUID:    "guid-1",
			PubDate: now,
		},
		PulledAt:  now,
		ExpiresAt: now.Add(24 * time.Hour),
	}
	if err := store.AddRawFeedItem(item); err != nil {
		t.Fatalf("AddRawFeedItem: %v", err)
	}

	items, err := store.GetRawFeedItems(10)
	if err != nil {
		t.Fatalf("GetRawFeedItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].FeedItem.Title != item.FeedItem.Title {
		t.Errorf("unexpected title: %q", items[0].FeedItem.Title)
	}
}

func TestCleanupExpiredRawFeedItems(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	now := time.Now()
	expired := models.RawFeedItem{
		FeedItem:  models.FeedItem{Title: "old", GUID: "g1"},
		PulledAt:  now.Add(-48 * time.Hour),
		ExpiresAt: now.Add(-24 * time.Hour), // already expired
	}
	fresh := models.RawFeedItem{
		FeedItem:  models.FeedItem{Title: "new", GUID: "g2"},
		PulledAt:  now,
		ExpiresAt: now.Add(24 * time.Hour),
	}
	_ = store.AddRawFeedItem(expired)
	_ = store.AddRawFeedItem(fresh)

	if err := store.CleanupExpiredRawFeedItems(); err != nil {
		t.Fatalf("CleanupExpiredRawFeedItems: %v", err)
	}

	items, _ := store.GetRawFeedItems(10)
	if len(items) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(items))
	}
	if items[0].FeedItem.Title != "new" {
		t.Errorf("unexpected remaining title: %q", items[0].FeedItem.Title)
	}
}

// ── AI score ─────────────────────────────────────────────────────────────────

func TestUpdateAIScore(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	torrent := createTestTorrent()
	_ = store.Add(torrent)

	if err := store.UpdateAIScore(1, 0.9, "looks great", 0.95, "title matched"); err != nil {
		t.Fatalf("UpdateAIScore: %v", err)
	}

	got, err := store.Get(1)
	if err != nil || got == nil {
		t.Fatalf("Get after UpdateAIScore: %v", err)
	}
	if got.AIScore != 0.9 {
		t.Errorf("expected AIScore 0.9, got %f", got.AIScore)
	}
	if !got.AIScored {
		t.Error("expected AIScored=true")
	}
}

// ── Settings ──────────────────────────────────────────────────────────────────

func TestGetSetSetting(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	// Missing key returns empty, no error.
	val, err := store.GetSetting("nonexistent")
	if err != nil {
		t.Fatalf("GetSetting missing key: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}

	if err := store.SetSetting("theme", "dark"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err = store.GetSetting("theme")
	if err != nil {
		t.Fatalf("GetSetting after set: %v", err)
	}
	if val != "dark" {
		t.Errorf("expected 'dark', got %q", val)
	}

	// Overwrite.
	_ = store.SetSetting("theme", "light")
	val, _ = store.GetSetting("theme")
	if val != "light" {
		t.Errorf("expected 'light' after overwrite, got %q", val)
	}
}

func TestGetAllSettings(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	_ = store.SetSetting("a", "1")
	_ = store.SetSetting("b", "2")

	all, err := store.GetAllSettings()
	if err != nil {
		t.Fatalf("GetAllSettings: %v", err)
	}
	if all["a"] != "1" || all["b"] != "2" {
		t.Errorf("unexpected settings: %v", all)
	}
}

// ── Approval quality profile ──────────────────────────────────────────────────

func TestGetApprovalQualityProfile_Empty(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	quality, codec, err := store.GetApprovalQualityProfile()
	if err != nil {
		t.Fatalf("GetApprovalQualityProfile: %v", err)
	}
	if quality != "" || codec != "" {
		t.Errorf("expected empty profile, got quality=%q codec=%q", quality, codec)
	}
}
