package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// The outside-assistant store (ADR 0231): the per-client tokens that let an
// MCP client read the boat, each token's tool list, and the activity log of
// what those clients asked for. It lives in helmcentral.sqlite beside the
// other stores and follows ADR 0184's convention: every write goes through
// RunTx, which holds the store mutex for the transaction's life.
//
// A token is a new credential type, separate from user logins (ADR 0111). It
// is read tier only. Only its SHA-256 is stored; the secret is shown once.

const (
	// mcpTokenPrefix marks a secret as a Helmcentral outside-assistant token,
	// so a leaked one is recognisable in a log or a paste.
	mcpTokenPrefix = "hcmcp_"
	// mcpActivityRetention is how long the activity log is kept.
	mcpActivityRetention = 30 * 24 * time.Hour
	// mcpTokenNameMax bounds a token's name.
	mcpTokenNameMax = 80
)

type mcpTokenRecord struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type mcpActivity struct {
	At         time.Time `json:"at"`
	Client     string    `json:"client"`
	Kind       string    `json:"kind"` // tool, resource or request
	Name       string    `json:"name"`
	Outcome    string    `json:"outcome"` // ok or an error class
	DurationMS int64     `json:"duration_ms"`
}

type outsideAssistantStore struct {
	mu   sync.Mutex
	db   *sql.DB
	now  func() time.Time
	rand io.Reader

	// lastStamp is when each token's last_used_at was last written, so a busy
	// client costs one write a minute and not one per request.
	stampMu   sync.Mutex
	lastStamp map[string]time.Time

	// Activity is written by one goroutine, off the request path.
	queue   chan mcpActivity
	pending sync.WaitGroup
	done    chan struct{}
	dropped atomic.Int64
	closed  sync.Once
}

var globalOutsideAssistantStore *outsideAssistantStore

const (
	mcpActivityBuffer = 256
	// mcpLastUsedEvery is the least time between two last_used_at writes for a token.
	mcpLastUsedEvery = time.Minute
)

func newOutsideAssistantStore(dbPath string) (*outsideAssistantStore, error) {
	return newOutsideAssistantStoreSized(dbPath, mcpActivityBuffer)
}

func newOutsideAssistantStoreSized(dbPath string, buffer int) (*outsideAssistantStore, error) {
	db, err := openHelmcentralDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := createOutsideAssistantSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	s := &outsideAssistantStore{
		db: db, now: time.Now, rand: rand.Reader,
		lastStamp: map[string]time.Time{},
		queue:     make(chan mcpActivity, buffer),
		done:      make(chan struct{}),
	}
	go s.writeActivity()
	return s, nil
}

// writeActivity is the one goroutine that writes queued activity.
func (s *outsideAssistantStore) writeActivity() {
	defer close(s.done)
	for e := range s.queue {
		if err := s.RecordActivity(e); err != nil {
			log.Printf("outside assistants: could not log %s %s for %s: %v", e.Kind, e.Name, e.Client, err)
		}
		s.pending.Done()
	}
}

// QueueActivity hands an entry to the writer. It never blocks the request. A
// full buffer is logged and counted, never dropped quietly.
func (s *outsideAssistantStore) QueueActivity(e mcpActivity) {
	s.pending.Add(1)
	select {
	case s.queue <- e:
	default:
		s.pending.Done()
		n := s.dropped.Add(1)
		log.Printf("outside assistants: ACTIVITY BUFFER FULL, dropped %s %s for %s (%d dropped since start)", e.Kind, e.Name, e.Client, n)
	}
}

// DroppedActivity is how many entries were dropped because the buffer was full.
func (s *outsideAssistantStore) DroppedActivity() int64 { return s.dropped.Load() }

func createOutsideAssistantSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mcp_tokens (
			id           TEXT PRIMARY KEY,
			name         TEXT NOT NULL,
			token_hash   TEXT NOT NULL UNIQUE,
			created_at   INTEGER NOT NULL,
			last_used_at INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_token_tools (
			token_id TEXT NOT NULL REFERENCES mcp_tokens(id) ON DELETE CASCADE,
			tool     TEXT NOT NULL,
			PRIMARY KEY (token_id, tool)
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_activity (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			at          INTEGER NOT NULL,
			client      TEXT NOT NULL,
			kind        TEXT NOT NULL,
			name        TEXT NOT NULL,
			outcome     TEXT NOT NULL,
			duration_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS mcp_activity_at ON mcp_activity (at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("create outside assistant schema: %w", err)
		}
	}
	return nil
}

func (s *outsideAssistantStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.closed.Do(func() {
		close(s.queue)
		<-s.done
	})
	return s.db.Close()
}

// RunTx runs fn in one transaction, holding the store mutex for its life
// (ADR 0184). fn gets the store's clock reading for the transaction.
func (s *outsideAssistantStore) RunTx(op string, fn func(tx *sql.Tx, now time.Time) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback()
	if err := fn(tx, s.now()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", op, err)
	}
	return nil
}

func hashMCPToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// CreateToken makes a token with its tool list seeded from tools, and returns
// the secret, which exists nowhere else afterwards.
func (s *outsideAssistantStore) CreateToken(name string, tools []string) (mcpTokenRecord, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return mcpTokenRecord{}, "", fmt.Errorf("%w: a token needs a name", errMCPTokenInvalid)
	}
	if len([]rune(name)) > mcpTokenNameMax {
		return mcpTokenRecord{}, "", fmt.Errorf("%w: a token's name can be at most %d characters", errMCPTokenInvalid, mcpTokenNameMax)
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return mcpTokenRecord{}, "", fmt.Errorf("create token: read random bytes: %w", err)
	}
	secret := mcpTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	rec := mcpTokenRecord{ID: uuid.NewString(), Name: name}
	err := s.RunTx("create outside assistant token", func(tx *sql.Tx, now time.Time) error {
		rec.CreatedAt = now.UTC().Truncate(time.Second)
		if _, err := tx.Exec(`INSERT INTO mcp_tokens (id, name, token_hash, created_at) VALUES (?, ?, ?, ?)`,
			rec.ID, rec.Name, hashMCPToken(secret), rec.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("create outside assistant token: %w", err)
		}
		return setTokenToolsTx(tx, rec.ID, tools)
	})
	if err != nil {
		return mcpTokenRecord{}, "", err
	}
	return rec, secret, nil
}

func setTokenToolsTx(tx *sql.Tx, id string, tools []string) error {
	if _, err := tx.Exec(`DELETE FROM mcp_token_tools WHERE token_id = ?`, id); err != nil {
		return fmt.Errorf("set token tools: %w", err)
	}
	seen := map[string]bool{}
	for _, t := range tools {
		if seen[t] {
			continue
		}
		seen[t] = true
		if _, err := tx.Exec(`INSERT INTO mcp_token_tools (token_id, tool) VALUES (?, ?)`, id, t); err != nil {
			return fmt.Errorf("set token tools: %w", err)
		}
	}
	return nil
}

// Authenticate finds the token whose hash is secret's SHA-256, by an indexed
// lookup. The hash is of 256 random bits, so how long the lookup takes says
// nothing useful about a guess; the match is still confirmed in constant time.
// A good token has its last-used time stamped, at most once a minute.
func (s *outsideAssistantStore) Authenticate(secret string) (mcpTokenRecord, bool, error) {
	if secret == "" {
		return mcpTokenRecord{}, false, nil
	}
	hash := hashMCPToken(secret)
	var rec mcpTokenRecord
	found := false
	err := s.RunTx("authenticate outside assistant", func(tx *sql.Tx, now time.Time) error {
		var stored string
		var created int64
		var used sql.NullInt64
		err := tx.QueryRow(`SELECT id, name, token_hash, created_at, last_used_at FROM mcp_tokens WHERE token_hash = ?`, hash).
			Scan(&rec.ID, &rec.Name, &stored, &created, &used)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(stored), []byte(hash)) != 1 {
			return nil
		}
		found = true
		rec.CreatedAt = time.Unix(created, 0).UTC()
		if used.Valid {
			t := time.Unix(used.Int64, 0).UTC()
			rec.LastUsedAt = &t
		}
		s.stampMu.Lock()
		last, seen := s.lastStamp[rec.ID]
		due := !seen || now.Sub(last) >= mcpLastUsedEvery
		if due {
			s.lastStamp[rec.ID] = now
		}
		s.stampMu.Unlock()
		if !due {
			return nil
		}
		t := now.UTC().Truncate(time.Second)
		if _, err := tx.Exec(`UPDATE mcp_tokens SET last_used_at = ? WHERE id = ?`, t.Unix(), rec.ID); err != nil {
			return err
		}
		rec.LastUsedAt = &t
		return nil
	})
	if err != nil {
		return mcpTokenRecord{}, false, fmt.Errorf("authenticate outside assistant: %w", err)
	}
	return rec, found, nil
}

// GetToken reads one token.
func (s *outsideAssistantStore) GetToken(id string) (mcpTokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec mcpTokenRecord
	var created int64
	var used sql.NullInt64
	err := s.db.QueryRow(`SELECT id, name, created_at, last_used_at FROM mcp_tokens WHERE id = ?`, id).Scan(&rec.ID, &rec.Name, &created, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpTokenRecord{}, errMCPTokenNotFound
	}
	if err != nil {
		return mcpTokenRecord{}, fmt.Errorf("read outside assistant token: %w", err)
	}
	rec.CreatedAt = time.Unix(created, 0).UTC()
	if used.Valid {
		t := time.Unix(used.Int64, 0).UTC()
		rec.LastUsedAt = &t
	}
	return rec, nil
}

func (s *outsideAssistantStore) ListTokens() ([]mcpTokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, name, created_at, last_used_at FROM mcp_tokens ORDER BY created_at, name`)
	if err != nil {
		return nil, fmt.Errorf("list outside assistant tokens: %w", err)
	}
	defer rows.Close()
	out := []mcpTokenRecord{}
	for rows.Next() {
		var rec mcpTokenRecord
		var created int64
		var used sql.NullInt64
		if err := rows.Scan(&rec.ID, &rec.Name, &created, &used); err != nil {
			return nil, fmt.Errorf("list outside assistant tokens: %w", err)
		}
		rec.CreatedAt = time.Unix(created, 0).UTC()
		if used.Valid {
			t := time.Unix(used.Int64, 0).UTC()
			rec.LastUsedAt = &t
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// RevokeToken deletes one token and its tool list. The token stops working at
// once. Activity it left behind keeps the client name it was logged under.
func (s *outsideAssistantStore) RevokeToken(id string) error {
	return s.RunTx("revoke outside assistant token", func(tx *sql.Tx, _ time.Time) error {
		res, err := tx.Exec(`DELETE FROM mcp_tokens WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("revoke outside assistant token: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errMCPTokenNotFound
		}
		s.stampMu.Lock()
		delete(s.lastStamp, id)
		s.stampMu.Unlock()
		return nil
	})
}

var (
	errMCPTokenNotFound = errors.New("no such token")
	// errMCPTokenInvalid marks a problem with what the caller asked for, as
	// opposed to a store or random-source failure.
	errMCPTokenInvalid = errors.New("invalid token request")
)

func (s *outsideAssistantStore) ToolsFor(id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT tool FROM mcp_token_tools WHERE token_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("read token tools: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("read token tools: %w", err)
		}
		out = append(out, t)
	}
	sort.Strings(out)
	return out, rows.Err()
}

// SetTools replaces a token's tool list. It does not judge the names: the
// caller (mcp_routes.go) rejects names that cannot be offered, and the server
// ignores any that slip through (mcp_tools.go).
func (s *outsideAssistantStore) SetTools(id string, tools []string) error {
	return s.RunTx("set outside assistant tools", func(tx *sql.Tx, _ time.Time) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM mcp_tokens WHERE id = ?`, id).Scan(&n); err != nil {
			return fmt.Errorf("set outside assistant tools: %w", err)
		}
		if n == 0 {
			return errMCPTokenNotFound
		}
		return setTokenToolsTx(tx, id, tools)
	})
}

func (s *outsideAssistantStore) RecordActivity(e mcpActivity) error {
	return s.RunTx("record outside assistant activity", func(tx *sql.Tx, _ time.Time) error {
		_, err := tx.Exec(`INSERT INTO mcp_activity (at, client, kind, name, outcome, duration_ms) VALUES (?, ?, ?, ?, ?, ?)`,
			e.At.UTC().Unix(), e.Client, e.Kind, e.Name, e.Outcome, e.DurationMS)
		if err != nil {
			return fmt.Errorf("record outside assistant activity: %w", err)
		}
		return nil
	})
}

// PruneActivity deletes activity older than the retention window and returns
// how many rows went.
func (s *outsideAssistantStore) PruneActivity() (int64, error) {
	var n int64
	err := s.RunTx("prune outside assistant activity", func(tx *sql.Tx, now time.Time) error {
		res, err := tx.Exec(`DELETE FROM mcp_activity WHERE at < ?`, now.Add(-mcpActivityRetention).Unix())
		if err != nil {
			return fmt.Errorf("prune outside assistant activity: %w", err)
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

func (s *outsideAssistantStore) ListActivity(limit int) ([]mcpActivity, error) {
	if limit <= 0 {
		limit = 50
	}
	s.pending.Wait() // what is queued is written first
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT at, client, kind, name, outcome, duration_ms FROM mcp_activity ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list outside assistant activity: %w", err)
	}
	defer rows.Close()
	out := []mcpActivity{}
	for rows.Next() {
		var e mcpActivity
		var at int64
		if err := rows.Scan(&at, &e.Client, &e.Kind, &e.Name, &e.Outcome, &e.DurationMS); err != nil {
			return nil, fmt.Errorf("list outside assistant activity: %w", err)
		}
		e.At = time.Unix(at, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// runMCPActivityPruner prunes the activity log once at start and then daily.
func runMCPActivityPruner(ctx context.Context, s *outsideAssistantStore) {
	prune := func() {
		n, err := s.PruneActivity()
		if err != nil {
			log.Printf("outside assistants: %v", err)
			return
		}
		if n > 0 {
			log.Printf("outside assistants: pruned %d activity rows older than %s", n, mcpActivityRetention)
		}
	}
	prune()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			prune()
		}
	}
}
