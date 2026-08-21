package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestPushStore(t *testing.T) *webPushSubscriptionStore {
	t.Helper()
	store, err := newWebPushSubscriptionStore(filepath.Join(t.TempDir(), "webpush.sqlite"))
	if err != nil {
		t.Fatalf("newWebPushSubscriptionStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func testSubscription(endpoint string) webPushSubscription {
	return webPushSubscription{
		Endpoint:       endpoint,
		P256dh:         "BF-test-p256dh",
		Auth:           "test-auth",
		Label:          "Helm tablet",
		UserAgent:      "Mozilla/5.0",
		VAPIDPublicKey: "key-A",
	}
}

// A browser re-issues subscribe() on every service-worker update and permission
// re-grant, always with the same endpoint. Without the endpoint being unique,
// one phone accumulates a row per update and receives that many copies of every
// alarm — so this is the load-bearing property of the schema, not tidiness.
func TestWebPushSubscriptionStoreUpsertReplacesSameEndpoint(t *testing.T) {
	store := newTestPushStore(t)

	first := testSubscription("https://push.example/aaa")
	if _, err := store.Upsert(first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := testSubscription("https://push.example/aaa")
	second.Label = "Gavin's iPhone"
	second.P256dh = "BF-rotated-key"
	if _, err := store.Upsert(second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	all, err := store.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("re-subscribing the same endpoint must replace, not duplicate: got %d rows", len(all))
	}
	if all[0].Label != "Gavin's iPhone" {
		t.Fatalf("label: got %q, want the newer one", all[0].Label)
	}
	if all[0].P256dh != "BF-rotated-key" {
		t.Fatalf("keys must be replaced on re-subscribe, got %q", all[0].P256dh)
	}
}

func TestWebPushSubscriptionStoreUpsertKeepsDistinctEndpointsApart(t *testing.T) {
	store := newTestPushStore(t)

	for _, endpoint := range []string{"https://push.example/a", "https://push.example/b"} {
		if _, err := store.Upsert(testSubscription(endpoint)); err != nil {
			t.Fatalf("upsert %s: %v", endpoint, err)
		}
	}

	all, err := store.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(all))
	}
}

// Unsubscribing is idempotent because the caller's goal is "this endpoint is
// gone", and it is — whether or not a row was there to delete.
func TestWebPushSubscriptionStoreDeleteByEndpointIsIdempotent(t *testing.T) {
	store := newTestPushStore(t)
	if _, err := store.Upsert(testSubscription("https://push.example/aaa")); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if err := store.DeleteByEndpoint("https://push.example/aaa"); err != nil {
			t.Fatalf("delete attempt %d: %v", attempt, err)
		}
	}

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the row gone, got %d", count)
	}
}

func TestWebPushSubscriptionStoreDeleteByID(t *testing.T) {
	store := newTestPushStore(t)
	saved, err := store.Upsert(testSubscription("https://push.example/aaa"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.ID == "" {
		t.Fatalf("Upsert must return the stored row with its id")
	}

	if err := store.DeleteByID(saved.ID); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
	if count, _ := store.Count(); count != 0 {
		t.Fatalf("expected the row gone, got %d", count)
	}
}

// If the secrets database is lost, a new VAPID keypair is minted while these
// rows still reference the old one, and every push then fails with
// 403 VapidPkHashMismatch forever. Discarding them at boot turns a permanent
// silent failure into one loud log line naming the fix.
func TestWebPushSubscriptionStoreDeletesRowsFromAnOlderVAPIDKey(t *testing.T) {
	store := newTestPushStore(t)

	for _, endpoint := range []string{"https://push.example/old1", "https://push.example/old2"} {
		if _, err := store.Upsert(testSubscription(endpoint)); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	current := testSubscription("https://push.example/current")
	current.VAPIDPublicKey = "key-B"
	if _, err := store.Upsert(current); err != nil {
		t.Fatalf("upsert current: %v", err)
	}

	removed, err := store.DeleteWhereKeyNot("key-B")
	if err != nil {
		t.Fatalf("DeleteWhereKeyNot: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 stale rows removed, got %d", removed)
	}

	all, err := store.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].Endpoint != "https://push.example/current" {
		t.Fatalf("only the current-key row should survive, got %+v", all)
	}
}

// A device that has quietly stopped working should be visible in the admin list
// before it is eventually pruned, so both outcomes are recorded.
func TestWebPushSubscriptionStoreMarkSuccessAndMarkError(t *testing.T) {
	store := newTestPushStore(t)
	saved, err := store.Upsert(testSubscription("https://push.example/aaa"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	at := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	if err := store.MarkSuccess(saved.ID, at); err != nil {
		t.Fatalf("MarkSuccess: %v", err)
	}
	all, _ := store.All()
	if all[0].LastSuccessAt == nil || !all[0].LastSuccessAt.Equal(at) {
		t.Fatalf("last_success_at: got %v, want %v", all[0].LastSuccessAt, at)
	}
	if all[0].LastError != "" {
		t.Fatalf("a success must clear the previous error, got %q", all[0].LastError)
	}

	if err := store.MarkError(saved.ID, "503 from push service"); err != nil {
		t.Fatalf("MarkError: %v", err)
	}
	all, _ = store.All()
	if all[0].LastError != "503 from push service" {
		t.Fatalf("last_error: got %q", all[0].LastError)
	}
}

// Every store in this package is nil-receiver safe so a failed open cannot
// panic the alarm path.
func TestWebPushSubscriptionStoreNilReceiverIsSafe(t *testing.T) {
	var store *webPushSubscriptionStore

	if _, err := store.All(); err != nil {
		t.Fatalf("All on nil store: %v", err)
	}
	if _, err := store.Upsert(testSubscription("https://push.example/a")); err != nil {
		t.Fatalf("Upsert on nil store: %v", err)
	}
	if err := store.DeleteByEndpoint("x"); err != nil {
		t.Fatalf("DeleteByEndpoint on nil store: %v", err)
	}
	if count, err := store.Count(); err != nil || count != 0 {
		t.Fatalf("Count on nil store: %d %v", count, err)
	}
}
