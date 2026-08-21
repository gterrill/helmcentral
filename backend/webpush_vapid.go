package main

import (
	"fmt"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// The VAPID keypair identifies Helmcentral to a push service (RFC 8292). Both
// halves live in the encrypted secrets store, and neither is a coreEnvSecretKey:
// nothing reads them via os.Getenv, and a private signing key must not become
// globally visible in the process environment where a WASM guest's config path
// could brush against it. Same reasoning as WEATHERKIT_*.
const (
	vapidPublicKeySecret  = "VAPID_PUBLIC_KEY"
	vapidPrivateKeySecret = "VAPID_PRIVATE_KEY"
)

// ensureVAPIDKeys returns the VAPID public key, generating the pair on first
// use, and reports it so callers can hand it to the browser.
//
// Generation is automatic rather than an admin button because a VAPID keypair
// is self-issued: unlike NTFY_TOKEN there is nowhere to obtain one from and
// nothing to paste, so a "Generate" button would be ceremony in front of
// crypto/rand.
//
// It never rotates. Rotating invalidates every registered device — they hold
// the public key they subscribed with — so a new pair is minted only when
// neither half exists.
func ensureVAPIDKeys(store *secretsStore) (string, error) {
	public, hasPublic, err := store.Get(vapidPublicKeySecret)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", vapidPublicKeySecret, err)
	}
	_, hasPrivate, err := store.Get(vapidPrivateKeySecret)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", vapidPrivateKeySecret, err)
	}

	if hasPublic && hasPrivate {
		return public, nil
	}

	// Half a keypair signs with one key and advertises another, which every
	// push service rejects with an opaque 403. Regenerating over the top would
	// hide that and silently disconnect any device registered against the
	// surviving half, so this stops instead.
	if hasPublic != hasPrivate {
		return "", fmt.Errorf(
			"web push: only one half of the VAPID keypair is stored (%s present: %v, %s present: %v) — "+
				"remove both from the secrets store to mint a fresh pair, which will require every device to re-subscribe",
			vapidPublicKeySecret, hasPublic, vapidPrivateKeySecret, hasPrivate)
	}

	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", fmt.Errorf("generate VAPID keys: %w", err)
	}

	// Private first: a crash between the two writes then leaves the "only one
	// half" state above, which reports itself, rather than a public key with no
	// signing key behind it.
	if err := store.Set(vapidPrivateKeySecret, privateKey); err != nil {
		return "", fmt.Errorf("store %s: %w", vapidPrivateKeySecret, err)
	}
	if err := store.Set(vapidPublicKeySecret, publicKey); err != nil {
		return "", fmt.Errorf("store %s: %w", vapidPublicKeySecret, err)
	}

	return publicKey, nil
}
