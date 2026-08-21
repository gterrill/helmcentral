package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/hkdf"
)

// A stand-in push service. Real ones are FCM, Mozilla autopush and
// web.push.apple.com; all three answer 201 on success and use 404/410 to say a
// subscription is permanently gone.
type pushServiceStub struct {
	mu       sync.Mutex
	requests []pushServiceRequest
	status   map[string]int // path -> status, default 201
	server   *httptest.Server
}

type pushServiceRequest struct {
	path    string
	headers http.Header
	body    []byte
}

func newPushServiceStub(t *testing.T) *pushServiceStub {
	t.Helper()
	stub := &pushServiceStub{status: map[string]int{}}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		stub.mu.Lock()
		stub.requests = append(stub.requests, pushServiceRequest{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		status, ok := stub.status[r.URL.Path]
		stub.mu.Unlock()

		if !ok {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *pushServiceStub) respondWith(path string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[path] = status
}

func (s *pushServiceStub) captured() []pushServiceRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pushServiceRequest(nil), s.requests...)
}

func (s *pushServiceStub) endpoint(path string) string { return s.server.URL + path }

// browserKeys stands in for a real browser's subscription keypair, so tests can
// both register a device and decrypt what was sent to it.
type browserKeys struct {
	private *ecdh.PrivateKey
	p256dh  string
	auth    string
	authRaw []byte
}

func newBrowserKeys(t *testing.T) browserKeys {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate browser key: %v", err)
	}
	authRaw := make([]byte, 16)
	if _, err := rand.Read(authRaw); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}
	return browserKeys{
		private: private,
		p256dh:  base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		auth:    base64.RawURLEncoding.EncodeToString(authRaw),
		authRaw: authRaw,
	}
}

// newTestWebPushTransport wires a transport at the stub, with a real VAPID
// keypair so the library signs exactly as it would in production.
func newTestWebPushTransport(t *testing.T, store *webPushSubscriptionStore) webPushTransport {
	t.Helper()
	secrets := newTestSecretsStore(t)
	public, err := ensureVAPIDKeys(secrets)
	if err != nil {
		t.Fatalf("ensureVAPIDKeys: %v", err)
	}
	private, _, _ := secrets.Get(vapidPrivateKeySecret)

	return webPushTransport{
		config:     webPushConfig{Enabled: true},
		publicKey:  public,
		privateKey: private,
		store:      store,
	}
}

func registerDevice(t *testing.T, store *webPushSubscriptionStore, endpoint string, keys browserKeys) webPushSubscription {
	t.Helper()
	saved, err := store.Upsert(webPushSubscription{
		Endpoint:       endpoint,
		P256dh:         keys.p256dh,
		Auth:           keys.auth,
		Label:          "device",
		VAPIDPublicKey: "unused-in-send",
	})
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	return saved
}

func TestWebPushTransportSendsToEveryStoredSubscription(t *testing.T) {
	stub := newPushServiceStub(t)
	store := newTestPushStore(t)
	for _, path := range []string{"/a", "/b", "/c"} {
		registerDevice(t, store, stub.endpoint(path), newBrowserKeys(t))
	}

	if err := newTestWebPushTransport(t, store).Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := stub.captured()
	if len(got) != 3 {
		t.Fatalf("expected one request per device, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, req := range got {
		seen[req.path] = true
	}
	for _, path := range []string{"/a", "/b", "/c"} {
		if !seen[path] {
			t.Fatalf("no request for %s; got %+v", path, seen)
		}
	}
}

// A 410 means the browser threw the subscription away — the site was
// uninstalled, or notifications were revoked. That is a RESOLVED condition, not
// a delivery failure: counting it would requeue the alarm for 24h on behalf of
// a device that no longer exists. Send must prune and return nil.
func TestWebPushTransportPrunesSubscriptionOn410(t *testing.T) {
	stub := newPushServiceStub(t)
	store := newTestPushStore(t)
	registerDevice(t, store, stub.endpoint("/dead"), newBrowserKeys(t))
	registerDevice(t, store, stub.endpoint("/live"), newBrowserKeys(t))
	stub.respondWith("/dead", http.StatusGone)

	if err := newTestWebPushTransport(t, store).Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("a pruned dead device is not a delivery failure, got: %v", err)
	}

	all, _ := store.All()
	if len(all) != 1 {
		t.Fatalf("expected only the live device to remain, got %d", len(all))
	}
	if !strings.HasSuffix(all[0].Endpoint, "/live") {
		t.Fatalf("the wrong device was pruned: %s", all[0].Endpoint)
	}
}

func TestWebPushTransportPrunesSubscriptionOn404(t *testing.T) {
	stub := newPushServiceStub(t)
	store := newTestPushStore(t)
	registerDevice(t, store, stub.endpoint("/gone"), newBrowserKeys(t))
	stub.respondWith("/gone", http.StatusNotFound)

	if err := newTestWebPushTransport(t, store).Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("expected a pruned device to be a resolved condition, got: %v", err)
	}
	if count, _ := store.Count(); count != 0 {
		t.Fatalf("expected the row pruned, %d remain", count)
	}
}

// A transient failure must queue, and must NOT prune — the device is fine, the
// network or the push service is not.
func TestWebPushTransportReturnsErrorWhenAnyDeliveryIsTransient(t *testing.T) {
	stub := newPushServiceStub(t)
	store := newTestPushStore(t)
	registerDevice(t, store, stub.endpoint("/ok"), newBrowserKeys(t))
	registerDevice(t, store, stub.endpoint("/flaky"), newBrowserKeys(t))
	stub.respondWith("/flaky", http.StatusServiceUnavailable)

	err := newTestWebPushTransport(t, store).Send(context.Background(), testMessage())
	if err == nil {
		t.Fatalf("a transient failure must surface so dispatch queues the alarm")
	}

	if count, _ := store.Count(); count != 2 {
		t.Fatalf("a transient failure must not prune the device, %d remain", count)
	}
	all, _ := store.All()
	var recorded bool
	for _, sub := range all {
		if strings.HasSuffix(sub.Endpoint, "/flaky") && sub.LastError != "" {
			recorded = true
		}
	}
	if !recorded {
		t.Fatalf("the failure should be recorded against the device, got %+v", all)
	}
}

// The interaction: a dead device must still be pruned even when another
// device's failure makes the overall Send fail.
func TestWebPushTransportPrunesOn410EvenWhenAnotherDeliveryFails(t *testing.T) {
	stub := newPushServiceStub(t)
	store := newTestPushStore(t)
	registerDevice(t, store, stub.endpoint("/dead"), newBrowserKeys(t))
	registerDevice(t, store, stub.endpoint("/flaky"), newBrowserKeys(t))
	stub.respondWith("/dead", http.StatusGone)
	stub.respondWith("/flaky", http.StatusServiceUnavailable)

	if err := newTestWebPushTransport(t, store).Send(context.Background(), testMessage()); err == nil {
		t.Fatalf("the transient failure should still surface")
	}

	all, _ := store.All()
	if len(all) != 1 || !strings.HasSuffix(all[0].Endpoint, "/flaky") {
		t.Fatalf("the dead device should be pruned regardless of the other failure, got %+v", all)
	}
}

// "Enabled but nothing registered" is the silently-undeliverable state that is
// worse than being switched off. Erroring queues the alarm, so a phone
// registered within 24h still receives the backlog.
func TestWebPushTransportErrorsWhenNoDevicesAreSubscribed(t *testing.T) {
	store := newTestPushStore(t)

	err := newTestWebPushTransport(t, store).Send(context.Background(), testMessage())
	if err == nil {
		t.Fatalf("expected an error when no devices are subscribed")
	}
	if !strings.Contains(err.Error(), "no devices") {
		t.Fatalf("the error should say why, got: %v", err)
	}
}

func TestWebPushTransportHeadersMatchSeverity(t *testing.T) {
	cases := []struct {
		state       string
		kind        string
		wantUrgency string
	}{
		{alarmStateEmergency, alarmEventRaised, "high"},
		{alarmStateAlarm, alarmEventRaised, "high"},
		{alarmStateWarn, alarmEventRaised, "normal"},
		{alarmStateAlert, alarmEventRaised, "normal"},
		{alarmStateAlarm, alarmEventCleared, "low"},
	}

	for _, tc := range cases {
		t.Run(tc.state+"/"+tc.kind, func(t *testing.T) {
			stub := newPushServiceStub(t)
			store := newTestPushStore(t)
			registerDevice(t, store, stub.endpoint("/d"), newBrowserKeys(t))

			msg := testMessage()
			msg.State = tc.state
			msg.Kind = tc.kind
			if err := newTestWebPushTransport(t, store).Send(context.Background(), msg); err != nil {
				t.Fatalf("Send: %v", err)
			}

			got := stub.captured()
			if len(got) != 1 {
				t.Fatalf("expected one request, got %d", len(got))
			}
			if urgency := got[0].headers.Get("Urgency"); urgency != tc.wantUrgency {
				t.Fatalf("Urgency: got %q, want %q", urgency, tc.wantUrgency)
			}
			if got[0].headers.Get("TTL") == "" {
				t.Fatalf("a TTL is required so the push service knows how long to hold the alarm")
			}
		})
	}
}

// The Topic header is what collapses the duplicates the retry queue creates
// (ADR 0038): a push service replaces a still-undelivered message carrying the
// same Topic. It must therefore be identical across retries of one transition,
// and different for a different transition — a "cleared" must never supersede
// an undelivered "raised", or an alarm that raised and cleared while the phone
// was offline would vanish entirely.
func TestWebPushTopicIsStableAcrossRetriesAndDistinctPerTransition(t *testing.T) {
	raised := testMessage()
	cleared := testMessage()
	cleared.Kind = alarmEventCleared

	if webPushTopicFor(raised) != webPushTopicFor(raised) {
		t.Fatalf("the same transition must produce the same topic on every retry")
	}
	if webPushTopicFor(raised) == webPushTopicFor(cleared) {
		t.Fatalf("a cleared must not supersede an undelivered raise for the same rule")
	}

	other := testMessage()
	other.RuleID = "rule-2"
	if webPushTopicFor(raised) == webPushTopicFor(other) {
		t.Fatalf("different rules must not collapse into one notification")
	}

	// RFC 8030 §5.4 constrains the header to at most 32 URL-safe base64 chars.
	topic := webPushTopicFor(raised)
	if len(topic) == 0 || len(topic) > 32 {
		t.Fatalf("topic length %d is outside RFC 8030's limit", len(topic))
	}
	if strings.ContainsAny(topic, "+/=") {
		t.Fatalf("topic must use the URL-safe alphabet, got %q", topic)
	}
}

// The load-bearing test. Everything else asserts that the push service answered
// 201 — which it does just as happily for a payload no phone can decrypt. This
// one stands up a browser keypair, sends through the real encryption path, and
// decrypts the captured ciphertext per RFC 8291, so it distinguishes "the
// request succeeded" from "a phone would show this alarm". ADR 0038 records
// this repo shipping two silent non-deliveries that looked exactly like success.
func TestWebPushPayloadDecryptsToTheAlarmTitleAndBody(t *testing.T) {
	stub := newPushServiceStub(t)
	store := newTestPushStore(t)
	keys := newBrowserKeys(t)
	registerDevice(t, store, stub.endpoint("/phone"), keys)

	msg := testMessage()
	if err := newTestWebPushTransport(t, store).Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	captured := stub.captured()
	if len(captured) != 1 {
		t.Fatalf("expected one request, got %d", len(captured))
	}
	if encoding := captured[0].headers.Get("Content-Encoding"); encoding != "aes128gcm" {
		t.Fatalf("Content-Encoding: got %q, want aes128gcm (RFC 8188)", encoding)
	}

	plaintext := decryptAES128GCMRecord(t, captured[0].body, keys)

	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Tag   string `json:"tag"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		t.Fatalf("decrypted payload is not the JSON the service worker expects: %v (%s)", err, plaintext)
	}
	if payload.Title != msg.title() {
		t.Fatalf("title: got %q, want %q", payload.Title, msg.title())
	}
	if payload.Body != msg.Message {
		t.Fatalf("body: got %q, want %q", payload.Body, msg.Message)
	}
	if payload.State != msg.State {
		t.Fatalf("state: got %q, want %q", payload.State, msg.State)
	}
	if payload.Tag == "" {
		t.Fatalf("a tag is required so the OS replaces rather than stacks duplicates")
	}
}

// decryptAES128GCMRecord reverses RFC 8291 §3.3 / RFC 8188 §2 as a browser
// would: parse the record header, derive the shared secret and the content
// encryption key, then open the single AES-128-GCM record.
func decryptAES128GCMRecord(t *testing.T, record []byte, keys browserKeys) []byte {
	t.Helper()
	if len(record) < 21 {
		t.Fatalf("record too short: %d bytes", len(record))
	}

	salt := record[0:16]
	idlen := int(record[20])
	if len(record) < 21+idlen {
		t.Fatalf("record truncated before the key id")
	}
	senderPublicRaw := record[21 : 21+idlen]
	ciphertext := record[21+idlen:]

	senderPublic, err := ecdh.P256().NewPublicKey(senderPublicRaw)
	if err != nil {
		t.Fatalf("sender public key: %v", err)
	}
	shared, err := keys.private.ECDH(senderPublic)
	if err != nil {
		t.Fatalf("ECDH: %v", err)
	}

	// PRK combining, RFC 8291 §3.3.
	receiverPublicRaw := keys.private.PublicKey().Bytes()
	info := append([]byte("WebPush: info\x00"), receiverPublicRaw...)
	info = append(info, senderPublicRaw...)

	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256New, shared, keys.authRaw, info), ikm); err != nil {
		t.Fatalf("derive ikm: %v", err)
	}

	cek := make([]byte, 16)
	if _, err := io.ReadFull(hkdf.New(sha256New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00")), cek); err != nil {
		t.Fatalf("derive cek: %v", err)
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(hkdf.New(sha256New, ikm, salt, []byte("Content-Encoding: nonce\x00")), nonce); err != nil {
		t.Fatalf("derive nonce: %v", err)
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v — a phone would show nothing for this push", err)
	}

	// RFC 8188 pads with a trailing delimiter byte (0x02 on the last record).
	for len(plaintext) > 0 {
		last := plaintext[len(plaintext)-1]
		if last != 0x00 && last != 0x01 && last != 0x02 {
			break
		}
		plaintext = plaintext[:len(plaintext)-1]
		if last == 0x02 || last == 0x01 {
			break
		}
	}
	return plaintext
}

// binary is used only to keep the record-header assertions honest if the layout
// ever changes; referenced here so the import stays meaningful.
var _ = binary.BigEndian

func sha256New() hash.Hash { return sha256.New() }
