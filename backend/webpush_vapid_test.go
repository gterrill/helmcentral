package main

import (
	"crypto/ecdh"
	"encoding/base64"
	"testing"
)

// The keypair is generated once and never again. Regenerating it invalidates
// every registered device — they encrypt against the public key they
// subscribed with — so idempotence is not a nicety here, it is what stops a
// future refactor silently disconnecting every phone on the boat at the next
// restart.
func TestEnsureVAPIDKeysGeneratesOnceAndIsIdempotent(t *testing.T) {
	store := newTestSecretsStore(t)

	first, err := ensureVAPIDKeys(store)
	if err != nil {
		t.Fatalf("first ensureVAPIDKeys: %v", err)
	}
	if first == "" {
		t.Fatalf("expected a public key to be returned")
	}

	firstPrivate, ok, err := store.Get("VAPID_PRIVATE_KEY")
	if err != nil || !ok {
		t.Fatalf("private key not stored: ok=%v err=%v", ok, err)
	}

	second, err := ensureVAPIDKeys(store)
	if err != nil {
		t.Fatalf("second ensureVAPIDKeys: %v", err)
	}
	if second != first {
		t.Fatalf("public key changed on second call: %q -> %q", first, second)
	}

	secondPrivate, _, _ := store.Get("VAPID_PRIVATE_KEY")
	if secondPrivate != firstPrivate {
		t.Fatalf("private key was regenerated, which would disconnect every device")
	}
}

func TestEnsureVAPIDKeysProducesAValidP256PublicKey(t *testing.T) {
	store := newTestSecretsStore(t)

	public, err := ensureVAPIDKeys(store)
	if err != nil {
		t.Fatalf("ensureVAPIDKeys: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(public)
	if err != nil {
		t.Fatalf("public key must be base64url without padding, got %q: %v", public, err)
	}
	// RFC 8291 fixes this: an uncompressed P-256 point, 65 bytes, 0x04 prefix.
	if len(raw) != 65 {
		t.Fatalf("public key: got %d bytes, want 65", len(raw))
	}
	if raw[0] != 0x04 {
		t.Fatalf("public key must be an uncompressed point (0x04), got 0x%02x", raw[0])
	}
	if _, err := ecdh.P256().NewPublicKey(raw); err != nil {
		t.Fatalf("public key is not a valid P-256 point: %v", err)
	}
}

// A half-written keypair would produce push requests signed by one key and
// addressed with another, which every push service rejects with an opaque 403.
// Failing loudly at boot is the fallback policy applied to a state mismatch.
func TestEnsureVAPIDKeysFailsWhenOnlyOneHalfIsStored(t *testing.T) {
	store := newTestSecretsStore(t)
	if err := store.Set("VAPID_PUBLIC_KEY", "orphaned-public-key"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, err := ensureVAPIDKeys(store); err == nil {
		t.Fatalf("expected a half-stored keypair to be reported, not silently regenerated")
	}
}

func TestVAPIDKeysAreRegisteredSecrets(t *testing.T) {
	for _, key := range []string{"VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY"} {
		if !isKnownSecretKey(key) {
			t.Fatalf("%s must be a known secret key or the settings handlers will reject it", key)
		}
	}

	// The private key must never reach the process environment, where a WASM
	// guest's config path could brush against it. Same reasoning as WEATHERKIT_*.
	for _, key := range coreEnvSecretKeys {
		if key == "VAPID_PRIVATE_KEY" {
			t.Fatalf("VAPID_PRIVATE_KEY must not be a core env secret")
		}
	}
}
