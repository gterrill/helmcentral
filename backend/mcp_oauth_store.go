package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"time"
)

// The OAuth clients that have registered themselves (RFC 7591) to sign in to
// the MCP endpoint (ADR 0233). They live in helmcentral.sqlite beside the
// outside-assistant tokens and follow the same rule: every write goes through
// RunTx. A client holds no secret. It is only a name and the redirect URIs it
// registered, kept so the sign-in page can name the app and refuse any other
// redirect, and the token it last signed in with.

const (
	mcpOAuthClientPrefix = "hcoc_"
	// mcpOAuthClientCap bounds the table. Registration is open to the public
	// when the endpoint is behind Funnel, so it cannot grow without limit.
	mcpOAuthClientCap = 50
	// mcpOAuthClientGrace is how long a client that has not signed in yet is
	// safe from eviction, so a flood of registrations cannot push out the one
	// the operator is in the middle of signing in with.
	mcpOAuthClientGrace = 15 * time.Minute
)

var errMCPOAuthClientsFull = errors.New("every OAuth client slot is held by a client that has signed in or is still within its first 15 minutes")

type mcpOAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	CreatedAt    time.Time
	AuthorizedAt *time.Time
}

// RegisterOAuthClient stores a new client. At the cap it first deletes the
// oldest client that can sign in to nothing: one whose token has been revoked,
// or one that never completed a sign-in and is older than mcpOAuthClientGrace.
// If every client holds a live token or is still within its grace, it refuses
// with errMCPOAuthClientsFull rather than evict one that works or one that may
// be mid-sign-in.
func (s *outsideAssistantStore) RegisterOAuthClient(name string, redirectURIs []string) (mcpOAuthClient, error) {
	raw := make([]byte, 24)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return mcpOAuthClient{}, fmt.Errorf("register oauth client: read random bytes: %w", err)
	}
	uris, err := json.Marshal(redirectURIs)
	if err != nil {
		return mcpOAuthClient{}, fmt.Errorf("register oauth client: %w", err)
	}
	c := mcpOAuthClient{ID: mcpOAuthClientPrefix + base64.RawURLEncoding.EncodeToString(raw), Name: name, RedirectURIs: redirectURIs}
	err = s.RunTx("register oauth client", func(tx *sql.Tx, now time.Time) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM mcp_oauth_clients`).Scan(&n); err != nil {
			return fmt.Errorf("register oauth client: %w", err)
		}
		if n >= mcpOAuthClientCap {
			cutoff := now.Add(-mcpOAuthClientGrace).Unix()
			res, err := tx.Exec(`DELETE FROM mcp_oauth_clients WHERE client_id = (
				SELECT client_id FROM mcp_oauth_clients
				WHERE (authorized_at IS NULL AND created_at < ?)
				   OR (authorized_at IS NOT NULL AND token_id NOT IN (SELECT id FROM mcp_tokens))
				ORDER BY created_at, rowid LIMIT 1)`, cutoff)
			if err != nil {
				return fmt.Errorf("register oauth client: %w", err)
			}
			if evicted, _ := res.RowsAffected(); evicted == 0 {
				return errMCPOAuthClientsFull
			}
			log.Printf("outside assistants: OAuth client table full, dropped the oldest client without a live sign-in")
		}
		c.CreatedAt = now.UTC().Truncate(time.Second)
		if _, err := tx.Exec(`INSERT INTO mcp_oauth_clients (client_id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`,
			c.ID, c.Name, string(uris), c.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("register oauth client: %w", err)
		}
		return nil
	})
	if err != nil {
		return mcpOAuthClient{}, err
	}
	return c, nil
}

// GetOAuthClient reads one client; found is false for an unknown id.
func (s *outsideAssistantStore) GetOAuthClient(id string) (mcpOAuthClient, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c mcpOAuthClient
	var uris string
	var created int64
	var authorized sql.NullInt64
	err := s.db.QueryRow(`SELECT client_id, name, redirect_uris, created_at, authorized_at FROM mcp_oauth_clients WHERE client_id = ?`, id).
		Scan(&c.ID, &c.Name, &uris, &created, &authorized)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpOAuthClient{}, false, nil
	}
	if err != nil {
		return mcpOAuthClient{}, false, fmt.Errorf("read oauth client: %w", err)
	}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return mcpOAuthClient{}, false, fmt.Errorf("read oauth client: redirect URIs: %w", err)
	}
	c.CreatedAt = time.Unix(created, 0).UTC()
	if authorized.Valid {
		t := time.Unix(authorized.Int64, 0).UTC()
		c.AuthorizedAt = &t
	}
	return c, true, nil
}

// MarkOAuthClientAuthorized records that a sign-in succeeded for the client
// with tokenID, which protects it from eviction at the cap while that token
// lives.
func (s *outsideAssistantStore) MarkOAuthClientAuthorized(id, tokenID string) error {
	return s.RunTx("mark oauth client authorized", func(tx *sql.Tx, now time.Time) error {
		if _, err := tx.Exec(`UPDATE mcp_oauth_clients SET authorized_at = ?, token_id = ? WHERE client_id = ?`, now.UTC().Unix(), tokenID, id); err != nil {
			return fmt.Errorf("mark oauth client authorized: %w", err)
		}
		return nil
	})
}
