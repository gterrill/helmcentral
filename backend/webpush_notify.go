package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// webPushSubscriber is the RFC 8292 "sub" claim. Apple's push service rejects a
// VAPID token without one; a project URL satisfies every push service without
// asking the operator for an email address that would then have to be stored,
// validated, and kept out of logs.
const webPushSubscriber = "https://github.com/gterrill/helmcentral"

// How long a push service should hold an undelivered alarm for a phone that is
// off or out of coverage. A cleared notification gets a much shorter life: one
// delivered twenty minutes late is actively misleading if the condition
// re-raised in between, and the alarm log holds the history regardless.
const (
	webPushTTLLive    = 3600
	webPushTTLCleared = 300
)

// webPushBodyLimit caps the one unbounded field. RFC 8291 guarantees only 4096
// bytes for the whole encrypted record, and computing the exact ciphertext
// budget for a limit nothing approaches would be false precision.
const webPushBodyLimit = 500

// webPushTransport delivers alarms to browsers registered through the Push API
// (ADR 0038). Unlike the other transports it fans out to N recipients, so its
// failure handling is per-device — see Send.
type webPushTransport struct {
	config     webPushConfig
	publicKey  string
	privateKey string
	store      *webPushSubscriptionStore

	// client is injectable for the same reason smtpTransport.send is: tests
	// drive the real RFC 8291 encryption path against a stub push service.
	// Nil uses notifyHTTPClient(), the same 10s client as the HTTP transports.
	client webpush.HTTPClient
}

func (t webPushTransport) ID() string { return transportWebPush }

// webPushPayload is what the service worker receives and renders. It is
// deliberately small and flat: the worker does no lookups, because it runs with
// no network guarantee at the moment a push arrives.
type webPushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
	State string `json:"state"`
	Kind  string `json:"kind"`
	URL   string `json:"url"`
}

// webPushTopicFor collapses duplicates at the push service. RFC 8030 §5.4: a
// push service replaces any still-undelivered message carrying the same Topic
// for the same subscription. The retry queue re-sends the whole message per
// transport (ADR 0038), so a phone that was offline across three retries
// receives one notification rather than three.
//
// Kind and At are in the hash deliberately. Keying on the rule alone would let
// a "cleared" supersede an undelivered "raised", so an alarm that raised and
// cleared while the phone was off would vanish entirely.
func webPushTopicFor(msg notificationMessage) string {
	sum := sha256.Sum256([]byte(msg.RuleID + "|" + msg.Kind + "|" + msg.At.UTC().Format(time.RFC3339)))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:22]
}

// webPushUrgencyFor mirrors ntfyPriorityFor: severity decides how hard the push
// service should work to wake a dozing phone.
func webPushUrgencyFor(msg notificationMessage) (urgency webpush.Urgency, ttl int) {
	if msg.Kind == alarmEventCleared {
		return webpush.UrgencyLow, webPushTTLCleared
	}
	switch msg.State {
	case alarmStateEmergency, alarmStateAlarm:
		return webpush.UrgencyHigh, webPushTTLLive
	default:
		return webpush.UrgencyNormal, webPushTTLLive
	}
}

func webPushPayloadFor(msg notificationMessage) webPushPayload {
	body := msg.Message
	if len([]rune(body)) > webPushBodyLimit {
		body = string([]rune(body)[:webPushBodyLimit])
	}

	return webPushPayload{
		Title: msg.title(),
		Body:  body,
		// The tag makes the OS replace an existing notification rather than
		// stack a second one, which is the device-side half of the
		// duplicate-collapsing story that Topic starts.
		Tag:   msg.RuleID + "|" + msg.Kind,
		State: msg.State,
		Kind:  msg.Kind,
		URL:   "/",
	}
}

// Send fans out to every registered device.
//
// The failure classification is the whole design. The retry queue works per
// transport, not per subscription, so what Send returns decides whether the
// alarm is re-sent to ALL devices:
//
//   - 404/410 means the browser threw the subscription away. That is a resolved
//     condition, not a delivery failure: the row is pruned and it does not make
//     Send fail, because requeuing on behalf of a device that no longer exists
//     would retry for 24h and re-notify every other device each time.
//   - Anything else keeps the row and fails, so dispatch queues the alarm.
//     Missing an alarm is worse than seeing one twice (ADR 0038 §6); Topic and
//     the notification tag collapse most of the duplicates that causes.
func (t webPushTransport) Send(ctx context.Context, msg notificationMessage) error {
	if t.publicKey == "" || t.privateKey == "" {
		return fmt.Errorf("web push: no VAPID keypair is configured")
	}

	subscriptions, err := t.store.All()
	if err != nil {
		return fmt.Errorf("web push: read devices: %w", err)
	}
	if len(subscriptions) == 0 {
		// Enabled but undeliverable is worse than switched off. Failing here
		// queues the alarm, so a device registered within the queue's 24h life
		// still receives the backlog.
		return fmt.Errorf("web push: no devices are subscribed")
	}

	payload, err := json.Marshal(webPushPayloadFor(msg))
	if err != nil {
		return fmt.Errorf("web push: encode payload: %w", err)
	}

	urgency, ttl := webPushUrgencyFor(msg)
	client := t.client
	if client == nil {
		client = notifyHTTPClient()
	}

	var failures []string
	for _, sub := range subscriptions {
		status, sendErr := t.deliver(ctx, client, payload, sub, urgency, ttl, msg)

		switch {
		case sendErr != nil:
			failures = append(failures, fmt.Sprintf("%s: %v", deviceName(sub), sendErr))
			t.recordError(sub, sendErr.Error())

		case status == http.StatusNotFound || status == http.StatusGone:
			// Loud, because a device silently disappearing is exactly what an
			// operator needs to know before they wonder why their phone is quiet.
			log.Printf("web push: pruned dead subscription for %s (%d from %s)", deviceName(sub), status, endpointHost(sub.Endpoint))
			if err := t.store.DeleteByEndpoint(sub.Endpoint); err != nil {
				log.Printf("web push: could not prune subscription: %v", err)
			}

		case status >= 200 && status < 300:
			if err := t.store.MarkSuccess(sub.ID, time.Now().UTC()); err != nil {
				log.Printf("web push: could not record delivery: %v", err)
			}

		default:
			reason := fmt.Sprintf("%d from %s", status, endpointHost(sub.Endpoint))
			failures = append(failures, fmt.Sprintf("%s: %s", deviceName(sub), reason))
			t.recordError(sub, reason)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("web push: %d of %d device(s) failed: %s",
			len(failures), len(subscriptions), strings.Join(failures, "; "))
	}
	return nil
}

func (t webPushTransport) deliver(
	ctx context.Context,
	client webpush.HTTPClient,
	payload []byte,
	sub webPushSubscription,
	urgency webpush.Urgency,
	ttl int,
	msg notificationMessage,
) (int, error) {
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient:      client,
		Subscriber:      webPushSubscriber,
		VAPIDPublicKey:  t.publicKey,
		VAPIDPrivateKey: t.privateKey,
		TTL:             ttl,
		Urgency:         urgency,
		Topic:           webPushTopicFor(msg),
	})
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

func (t webPushTransport) recordError(sub webPushSubscription, reason string) {
	if err := t.store.MarkError(sub.ID, reason); err != nil {
		log.Printf("web push: could not record device error: %v", err)
	}
}

// deviceName prefers the operator's label and falls back to the user agent, so
// a device nobody bothered to name is still identifiable in a log line.
func deviceName(sub webPushSubscription) string {
	if label := strings.TrimSpace(sub.Label); label != "" {
		return label
	}
	if ua := strings.TrimSpace(sub.UserAgent); ua != "" {
		return ua
	}
	return endpointHost(sub.Endpoint)
}

// endpointHost keeps the push service identifiable in errors without logging
// the endpoint itself, which is a bearer capability to notify that device.
func endpointHost(endpoint string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	if slash := strings.IndexByte(trimmed, '/'); slash >= 0 {
		return trimmed[:slash]
	}
	return trimmed
}
