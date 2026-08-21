package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func withGlobalPushStore(t *testing.T) *webPushSubscriptionStore {
	t.Helper()
	store := newTestPushStore(t)
	original := globalWebPushSubscriptionStore
	globalWebPushSubscriptionStore = store
	t.Cleanup(func() { globalWebPushSubscriptionStore = original })
	return store
}

func withGlobalSecrets(t *testing.T) *secretsStore {
	t.Helper()
	store := newTestSecretsStore(t)
	original := globalSecretsStore
	globalSecretsStore = store
	t.Cleanup(func() { globalSecretsStore = original })
	return store
}

func pushRequest(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var req *http.Request
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req = httptest.NewRequest(method, path, strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

func validSubscribeBody() map[string]any {
	return map[string]any{
		"endpoint": "https://push.example/abc",
		"keys": map[string]string{
			// 65 bytes starting 0x04, and 16 bytes — both fixed by RFC 8291.
			"p256dh": validP256dhForTest(),
			"auth":   validAuthForTest(),
		},
		"label":      "Gavin's iPhone",
		"user_agent": "Mozilla/5.0",
	}
}

// Malformed keys would otherwise surface as a panic deep inside the encryption
// path at alarm time; rejecting them at request time is the fail-fast version.
func TestSubscribeWebPushRejectsInvalidBodies(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"no endpoint", map[string]any{"keys": map[string]string{"p256dh": validP256dhForTest(), "auth": validAuthForTest()}}},
		{"no p256dh", map[string]any{"endpoint": "https://push.example/a", "keys": map[string]string{"auth": validAuthForTest()}}},
		{"no auth", map[string]any{"endpoint": "https://push.example/a", "keys": map[string]string{"p256dh": validP256dhForTest()}}},
		{"http endpoint", map[string]any{"endpoint": "http://push.example/a", "keys": map[string]string{"p256dh": validP256dhForTest(), "auth": validAuthForTest()}}},
		{"short p256dh", map[string]any{"endpoint": "https://push.example/a", "keys": map[string]string{"p256dh": "AAAA", "auth": validAuthForTest()}}},
		{"short auth", map[string]any{"endpoint": "https://push.example/a", "keys": map[string]string{"p256dh": validP256dhForTest(), "auth": "AA"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withGlobalPushStore(t)
			c, rec := pushRequest(t, http.MethodPost, "/api/alarm-transports/webpush/subscribe", tc.body)

			if err := subscribeWebPushHandler(c); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSubscribeWebPushStoresTheDeviceAndIsIdempotent(t *testing.T) {
	store := withGlobalPushStore(t)
	secrets := withGlobalSecrets(t)
	if _, err := ensureVAPIDKeys(secrets); err != nil {
		t.Fatalf("ensureVAPIDKeys: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		c, rec := pushRequest(t, http.MethodPost, "/api/alarm-transports/webpush/subscribe", validSubscribeBody())
		if err := subscribeWebPushHandler(c); err != nil {
			t.Fatalf("handler: %v", err)
		}
		if rec.Code != http.StatusCreated {
			t.Fatalf("attempt %d status: got %d, want 201 (body %s)", attempt, rec.Code, rec.Body.String())
		}
	}

	count, _ := store.Count()
	if count != 1 {
		t.Fatalf("re-subscribing the same endpoint must not duplicate, got %d rows", count)
	}
}

// The keys are a capability to encrypt to that device. They go in and are never
// read back out over HTTP.
func TestSubscribeWebPushNeverEchoesTheKeysBack(t *testing.T) {
	withGlobalPushStore(t)
	secrets := withGlobalSecrets(t)
	if _, err := ensureVAPIDKeys(secrets); err != nil {
		t.Fatalf("ensureVAPIDKeys: %v", err)
	}

	c, rec := pushRequest(t, http.MethodPost, "/api/alarm-transports/webpush/subscribe", validSubscribeBody())
	if err := subscribeWebPushHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}

	body := rec.Body.String()
	for _, secret := range []string{validP256dhForTest(), validAuthForTest(), "https://push.example/abc"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response must not echo %q back: %s", secret, body)
		}
	}
}

func TestUnsubscribeWebPushRemovesTheRowAndIsIdempotent(t *testing.T) {
	store := withGlobalPushStore(t)
	if _, err := store.Upsert(testSubscription("https://push.example/abc")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		c, rec := pushRequest(t, http.MethodPost, "/api/alarm-transports/webpush/unsubscribe",
			map[string]string{"endpoint": "https://push.example/abc"})
		if err := unsubscribeWebPushHandler(c); err != nil {
			t.Fatalf("handler: %v", err)
		}
		if rec.Code != http.StatusNoContent {
			t.Fatalf("attempt %d status: got %d, want 204", attempt, rec.Code)
		}
	}

	if count, _ := store.Count(); count != 0 {
		t.Fatalf("expected the row gone, got %d", count)
	}
}

func TestWebPushKeyHandlerReturnsPublicKeyAndDeviceCount(t *testing.T) {
	store := withGlobalPushStore(t)
	secrets := withGlobalSecrets(t)
	public, err := ensureVAPIDKeys(secrets)
	if err != nil {
		t.Fatalf("ensureVAPIDKeys: %v", err)
	}
	if _, err := store.Upsert(testSubscription("https://push.example/abc")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	c, rec := pushRequest(t, http.MethodGet, "/api/alarm-transports/webpush/key", nil)
	if err := webPushKeyHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d (body %s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		PublicKey         string `json:"public_key"`
		SubscribedDevices int    `json:"subscribed_devices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.PublicKey != public {
		t.Fatalf("public_key: got %q, want %q", payload.PublicKey, public)
	}
	if payload.SubscribedDevices != 1 {
		t.Fatalf("subscribed_devices: got %d, want 1", payload.SubscribedDevices)
	}
}

// Handing the browser an empty key would make subscribe() fail with an opaque
// AbortError. Failing the request says what is actually wrong.
func TestWebPushKeyHandlerIs404WhenNoKeypairExists(t *testing.T) {
	withGlobalPushStore(t)
	withGlobalSecrets(t)

	c, rec := pushRequest(t, http.MethodGet, "/api/alarm-transports/webpush/key", nil)
	if err := webPushKeyHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

func TestListWebPushSubscriptionsOmitsSecrets(t *testing.T) {
	store := withGlobalPushStore(t)
	if _, err := store.Upsert(testSubscription("https://push.example/abc")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	c, rec := pushRequest(t, http.MethodGet, "/api/alarm-transports/webpush/subscriptions", nil)
	if err := listWebPushSubscriptionsHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "BF-test-p256dh") || strings.Contains(rec.Body.String(), "push.example") {
		t.Fatalf("the device list must not expose keys or endpoints: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Helm tablet") {
		t.Fatalf("expected the device label in the list: %s", rec.Body.String())
	}
}

// Fixtures shaped like the real thing: RFC 8291 fixes p256dh at a 65-byte
// uncompressed P-256 point and auth at 16 bytes.
func validP256dhForTest() string {
	raw := make([]byte, 65)
	raw[0] = 0x04
	return base64.RawURLEncoding.EncodeToString(raw)
}

func validAuthForTest() string {
	return base64.RawURLEncoding.EncodeToString(make([]byte, 16))
}
