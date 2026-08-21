package main

import (
	"encoding/base64"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// RFC 8291 fixes both key sizes: p256dh is an uncompressed P-256 point and auth
// is a 16-byte secret. Checking them here turns what would otherwise be a
// failure deep inside the encryption path at alarm time into a 400 at
// registration time.
const (
	webPushP256dhBytes = 65
	webPushAuthBytes   = 16
)

// webPushSubscribeRequest is PushSubscription.toJSON() plus the two fields
// Helmcentral adds, so the frontend passes the browser's own object through
// almost untouched.
type webPushSubscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Label     string `json:"label"`
	UserAgent string `json:"user_agent"`
}

// webPushKeyHandler hands the browser the application server key it needs for
// PushManager.subscribe, plus the device count the settings UI shows.
func webPushKeyHandler(c echo.Context) error {
	public := secretOrEmpty(vapidPublicKeySecret)
	if public == "" {
		// Never an empty-string key: the browser would fail subscribe() with an
		// opaque AbortError instead of anything an operator could act on.
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "web push keys are not available",
		})
	}

	count, err := globalWebPushSubscriptionStore.Count()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"public_key":         public,
		"subscribed_devices": count,
	})
}

func subscribeWebPushHandler(c echo.Context) error {
	var req webPushSubscribeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	req.Endpoint = strings.TrimSpace(req.Endpoint)
	if req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "web push subscription requires an endpoint and both keys",
		})
	}
	// A non-https endpoint is a broken or hostile client: no push service
	// issues one.
	if !strings.HasPrefix(req.Endpoint, "https://") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "web push endpoint must be https"})
	}
	if !isBase64URLOfLength(req.Keys.P256dh, webPushP256dhBytes) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "web push p256dh key is malformed"})
	}
	if !isBase64URLOfLength(req.Keys.Auth, webPushAuthBytes) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "web push auth key is malformed"})
	}

	stored, err := globalWebPushSubscriptionStore.Upsert(webPushSubscription{
		Endpoint:  req.Endpoint,
		P256dh:    req.Keys.P256dh,
		Auth:      req.Keys.Auth,
		Label:     strings.TrimSpace(req.Label),
		UserAgent: strings.TrimSpace(req.UserAgent),
		// Recorded so a later VAPID mismatch is detectable rather than a
		// permanent silent 403 (see DeleteWhereKeyNot).
		VAPIDPublicKey: secretOrEmpty(vapidPublicKeySecret),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	log.Printf("web push: registered device %q", deviceName(stored))

	// The stored row's json tags omit the endpoint and both keys: they are a
	// capability to notify that device and only ever travel inbound.
	return c.JSON(http.StatusCreated, stored)
}

// unsubscribeWebPushHandler takes the endpoint in the body rather than the path.
// Endpoints are full URLs of slashes and colons, and Echo hands path params to
// handlers still percent-encoded (ADR 0038's addendum records this repo losing
// time to exactly that).
func unsubscribeWebPushHandler(c echo.Context) error {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	if strings.TrimSpace(req.Endpoint) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "an endpoint is required"})
	}

	// Idempotent: the caller's goal is that the endpoint is gone, and a row
	// that was never there already satisfies it.
	if err := globalWebPushSubscriptionStore.DeleteByEndpoint(strings.TrimSpace(req.Endpoint)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func listWebPushSubscriptionsHandler(c echo.Context) error {
	subscriptions, err := globalWebPushSubscriptionStore.All()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if subscriptions == nil {
		subscriptions = []webPushSubscription{}
	}
	return c.JSON(http.StatusOK, map[string]any{"devices": subscriptions})
}

func deleteWebPushSubscriptionHandler(c echo.Context) error {
	if err := globalWebPushSubscriptionStore.DeleteByID(c.Param("id")); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func isBase64URLOfLength(value string, want int) bool {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		// Some browsers pad; accept that rather than reject a valid key.
		raw, err = base64.URLEncoding.DecodeString(value)
		if err != nil {
			return false
		}
	}
	return len(raw) == want
}
