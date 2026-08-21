package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// webPushSubscription is one browser on one device, as registered by the Push
// API. P256dh and Auth are the browser's own encryption keys — Helmcentral
// never generates them and never sends them back out.
type webPushSubscription struct {
	ID        string `json:"id"`
	Endpoint  string `json:"-"`
	P256dh    string `json:"-"`
	Auth      string `json:"-"`
	Label     string `json:"label"`
	UserAgent string `json:"user_agent"`

	// VAPIDPublicKey records which keypair this device was registered against.
	// A mismatch is unrecoverable (see DeleteWhereKeyNot), so it has to be
	// stored rather than assumed.
	VAPIDPublicKey string `json:"-"`

	CreatedAt     time.Time  `json:"created_at"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// webPushSubscriptionStore owns its own SQLite file rather than sharing
// alarm-log.sqlite. Everything in that file is prunable operational data — a
// capped log view and a queue that expires itself at 24h — whereas these rows
// are durable device registrations whose loss cannot be recovered without
// physically revisiting every phone. Mixing the two lifetimes would make
// "can I delete alarm-log.sqlite?" a dangerous question that today has a safe
// answer.
type webPushSubscriptionStore struct {
	mu sync.Mutex
	db *sql.DB
}

var globalWebPushSubscriptionStore *webPushSubscriptionStore

func webPushDBPath() string {
	return cacheFilePath("WEBPUSH_DB_PATH", "data/webpush-subscriptions.sqlite")
}

func newWebPushSubscriptionStore(dbPath string) (*webPushSubscriptionStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("web push store dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open web push store: %w", err)
	}
	// modernc/sqlite surfaces concurrent writes as "database is locked"; the
	// other stores in this package serialize the same way.
	db.SetMaxOpenConns(1)

	// endpoint UNIQUE is load-bearing. A browser re-issues subscribe() on every
	// service-worker update and permission re-grant, always with the same
	// endpoint; without the constraint (and the ON CONFLICT upsert below) one
	// phone accumulates a row per update and receives that many copies of every
	// alarm.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS push_subscriptions (
			id               TEXT PRIMARY KEY,
			endpoint         TEXT NOT NULL UNIQUE,
			p256dh           TEXT NOT NULL,
			auth             TEXT NOT NULL,
			label            TEXT NOT NULL DEFAULT '',
			user_agent       TEXT NOT NULL DEFAULT '',
			vapid_public_key TEXT NOT NULL,
			created_at       INTEGER NOT NULL,
			last_success_at  INTEGER,
			last_error       TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create push subscriptions table: %w", err)
	}

	return &webPushSubscriptionStore{db: db}, nil
}

func (s *webPushSubscriptionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Upsert registers a device, replacing any existing row for the same endpoint.
func (s *webPushSubscriptionStore) Upsert(sub webPushSubscription) (webPushSubscription, error) {
	if s == nil {
		return sub, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if sub.ID == "" {
		sub.ID = uuid.NewString()
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}

	// The keys, label and user agent are all refreshed on conflict: a
	// re-subscribe usually means the browser rotated its keys, and keeping the
	// old ones would encrypt to a device that can no longer decrypt. created_at
	// and id are deliberately left at their original values so a device keeps
	// its identity across re-subscribes.
	_, err := s.db.Exec(`
		INSERT INTO push_subscriptions
			(id, endpoint, p256dh, auth, label, user_agent, vapid_public_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh           = excluded.p256dh,
			auth             = excluded.auth,
			label            = excluded.label,
			user_agent       = excluded.user_agent,
			vapid_public_key = excluded.vapid_public_key,
			last_error       = ''`,
		sub.ID, sub.Endpoint, sub.P256dh, sub.Auth, sub.Label, sub.UserAgent,
		sub.VAPIDPublicKey, sub.CreatedAt.UTC().Unix())
	if err != nil {
		return webPushSubscription{}, fmt.Errorf("upsert push subscription: %w", err)
	}

	stored, err := s.byEndpointLocked(sub.Endpoint)
	if err != nil {
		return webPushSubscription{}, err
	}
	return stored, nil
}

func (s *webPushSubscriptionStore) byEndpointLocked(endpoint string) (webPushSubscription, error) {
	row := s.db.QueryRow(`
		SELECT id, endpoint, p256dh, auth, label, user_agent, vapid_public_key,
		       created_at, last_success_at, last_error
		FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return scanWebPushSubscription(row)
}

type webPushRowScanner interface {
	Scan(dest ...any) error
}

func scanWebPushSubscription(row webPushRowScanner) (webPushSubscription, error) {
	var (
		sub           webPushSubscription
		createdAt     int64
		lastSuccessAt sql.NullInt64
	)
	if err := row.Scan(&sub.ID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.Label,
		&sub.UserAgent, &sub.VAPIDPublicKey, &createdAt, &lastSuccessAt, &sub.LastError); err != nil {
		return webPushSubscription{}, fmt.Errorf("scan push subscription: %w", err)
	}

	sub.CreatedAt = time.Unix(createdAt, 0).UTC()
	if lastSuccessAt.Valid {
		at := time.Unix(lastSuccessAt.Int64, 0).UTC()
		sub.LastSuccessAt = &at
	}
	return sub, nil
}

// All returns every registered device, newest first.
func (s *webPushSubscriptionStore) All() ([]webPushSubscription, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT id, endpoint, p256dh, auth, label, user_agent, vapid_public_key,
		       created_at, last_success_at, last_error
		FROM push_subscriptions ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("read push subscriptions: %w", err)
	}
	defer rows.Close()

	var out []webPushSubscription
	for rows.Next() {
		sub, err := scanWebPushSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// DeleteByEndpoint is idempotent: the caller's goal is that the endpoint is
// gone, and a row that was never there already satisfies it.
func (s *webPushSubscriptionStore) DeleteByEndpoint(endpoint string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint); err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

func (s *webPushSubscriptionStore) DeleteByID(id string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

func (s *webPushSubscriptionStore) MarkSuccess(id string, at time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(
		`UPDATE push_subscriptions SET last_success_at = ?, last_error = '' WHERE id = ?`,
		at.UTC().Unix(), id); err != nil {
		return fmt.Errorf("mark push subscription success: %w", err)
	}
	return nil
}

func (s *webPushSubscriptionStore) MarkError(id string, message string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(
		`UPDATE push_subscriptions SET last_error = ? WHERE id = ?`, message, id); err != nil {
		return fmt.Errorf("mark push subscription error: %w", err)
	}
	return nil
}

// DeleteWhereKeyNot drops every device registered against a different VAPID
// keypair, reporting how many it removed.
//
// Those rows are unrecoverable rather than merely stale: the push service
// answers 403 VapidPkHashMismatch for them forever, and no retry can fix it.
// Discarding them at boot converts a permanent silent failure into one loud
// log line telling the operator those devices must re-subscribe.
func (s *webPushSubscriptionStore) DeleteWhereKeyNot(publicKey string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE vapid_public_key != ?`, publicKey)
	if err != nil {
		return 0, fmt.Errorf("discard stale push subscriptions: %w", err)
	}
	return result.RowsAffected()
}

func (s *webPushSubscriptionStore) Count() (int, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count push subscriptions: %w", err)
	}
	return count, nil
}
