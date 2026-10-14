package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The track point log, passages and cruises (ADR 0173), in helmcentral.sqlite.

var errPassageNotFound = errors.New("passage not found")

// globalPassageStore is opened once in main() and shared by the track poller
// (writes) and the /api/passages handlers (reads).
var globalPassageStore *passageStore

// recordedTrackPoint is one row of the track point log. A nil reading is
// absent: it was too old when the point was taken, or the boat does not
// publish it. Imported points carry only position and speed.
type recordedTrackPoint struct {
	ID      int64     `json:"-"`
	At      time.Time `json:"at"`
	Segment int       `json:"segment"`
	Lat     float64   `json:"lat"`
	Lon     float64   `json:"lon"`

	SOGKts   *float64 `json:"sog_kts"`
	COGDeg   *float64 `json:"cog_deg"`
	TWSKts   *float64 `json:"tws_kts"`
	GustKts  *float64 `json:"gust_kts"`
	SoCPct   *float64 `json:"soc_pct"`
	FuelLph  *float64 `json:"fuel_lph"`
	WaterL   *float64 `json:"water_l"`
	GenState *string  `json:"gen_state"`

	// AnchorSetAt is when the anchor watch in force at this moment was set.
	// The detector ends a passage when it sees a value later than the
	// passage start.
	AnchorSetAt *time.Time `json:"-"`

	// Source is trackSourceRecorded for points Helmcentral took itself and
	// trackSourceImported for GPX imports. Imported points never feed the
	// live recorder or the live detector.
	Source string `json:"-"`
}

const (
	trackSourceRecorded = "recorded"
	trackSourceImported = "imported"
)

type passageRecord struct {
	ID        string    `json:"id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	FromName  string    `json:"from_name"`
	ToName    string    `json:"to_name"`
	Title     string    `json:"title"`
	Notes     string    `json:"notes"`
	Hidden    bool      `json:"hidden"`
	Edited    bool      `json:"edited"`
	CruiseID  *string   `json:"cruise_id"`
	CreatedAt time.Time `json:"created_at"`
}

type cruiseRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
}

type passageStore struct {
	db *sql.DB
}

func newPassageStore(dbPath string) (*passageStore, error) {
	db, err := openHelmcentralDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := createPassageSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &passageStore{db: db}, nil
}

func (s *passageStore) close() error { return s.db.Close() }

func createPassageSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS track_points (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			t_ms INTEGER NOT NULL,
			segment INTEGER NOT NULL,
			lat REAL NOT NULL,
			lon REAL NOT NULL,
			sog_kts REAL,
			cog_deg REAL,
			tws_kts REAL,
			gust_kts REAL,
			soc_pct REAL,
			fuel_lph REAL,
			water_l REAL,
			gen_state TEXT,
			anchor_set_ms INTEGER,
			source TEXT NOT NULL DEFAULT 'recorded'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_track_points_t ON track_points(t_ms)`,
		`CREATE TABLE IF NOT EXISTS cruises (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			notes TEXT NOT NULL DEFAULT '',
			created_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS passages (
			id TEXT PRIMARY KEY,
			start_ms INTEGER NOT NULL,
			end_ms INTEGER NOT NULL,
			from_name TEXT NOT NULL DEFAULT '',
			to_name TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			hidden INTEGER NOT NULL DEFAULT 0,
			edited INTEGER NOT NULL DEFAULT 0,
			cruise_id TEXT REFERENCES cruises(id) ON DELETE SET NULL,
			created_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_passages_start ON passages(start_ms)`,
		`CREATE TABLE IF NOT EXISTS water_rise_labels (
			cruise_id TEXT NOT NULL REFERENCES cruises(id) ON DELETE CASCADE,
			at_ms INTEGER NOT NULL,
			label TEXT NOT NULL,
			PRIMARY KEY (cruise_id, at_ms)
		)`,
		`CREATE TABLE IF NOT EXISTS passage_detector_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			resume_ms INTEGER NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("create passage schema: %w", err)
		}
	}
	return addTrackPointSourceColumn(db)
}

// addTrackPointSourceColumn upgrades a track_points table created before GPX
// import existed.
func addTrackPointSourceColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(track_points)`)
	if err != nil {
		return fmt.Errorf("inspect track_points: %w", err)
	}
	has := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("inspect track_points: %w", err)
		}
		if name == "source" {
			has = true
		}
	}
	rows.Close()
	if has {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE track_points ADD COLUMN source TEXT NOT NULL DEFAULT 'recorded'`); err != nil {
		return fmt.Errorf("add track_points.source: %w", err)
	}
	return nil
}

func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullToPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (s *passageStore) AppendTrackPoint(p recordedTrackPoint) error {
	return insertTrackPoint(s.db, p)
}

func insertTrackPoint(db sqlExecer, p recordedTrackPoint) error {
	var gen, anchor any
	if p.GenState != nil {
		gen = *p.GenState
	}
	if p.AnchorSetAt != nil {
		anchor = p.AnchorSetAt.UnixMilli()
	}
	source := p.Source
	if source == "" {
		source = trackSourceRecorded
	}
	_, err := db.Exec(`INSERT INTO track_points
		(t_ms, segment, lat, lon, sog_kts, cog_deg, tws_kts, gust_kts, soc_pct, fuel_lph, water_l, gen_state, anchor_set_ms, source)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.At.UnixMilli(), p.Segment, p.Lat, p.Lon,
		nullFloat(p.SOGKts), nullFloat(p.COGDeg), nullFloat(p.TWSKts), nullFloat(p.GustKts),
		nullFloat(p.SoCPct), nullFloat(p.FuelLph), nullFloat(p.WaterL), gen, anchor, source)
	if err != nil {
		return fmt.Errorf("append track point: %w", err)
	}
	return nil
}

const trackPointColumns = `id, t_ms, segment, lat, lon, sog_kts, cog_deg, tws_kts, gust_kts, soc_pct, fuel_lph, water_l, gen_state, anchor_set_ms, source`

func scanTrackPoint(r rowScanner) (recordedTrackPoint, error) {
	var p recordedTrackPoint
	var ms int64
	var sog, cog, tws, gust, soc, fuel, water sql.NullFloat64
	var gen sql.NullString
	var anchor sql.NullInt64
	if err := r.Scan(&p.ID, &ms, &p.Segment, &p.Lat, &p.Lon, &sog, &cog, &tws, &gust, &soc, &fuel, &water, &gen, &anchor, &p.Source); err != nil {
		return p, err
	}
	p.At = time.UnixMilli(ms).UTC()
	p.SOGKts, p.COGDeg, p.TWSKts, p.GustKts = nullToPtr(sog), nullToPtr(cog), nullToPtr(tws), nullToPtr(gust)
	p.SoCPct, p.FuelLph, p.WaterL = nullToPtr(soc), nullToPtr(fuel), nullToPtr(water)
	if gen.Valid {
		g := gen.String
		p.GenState = &g
	}
	if anchor.Valid {
		t := time.UnixMilli(anchor.Int64).UTC()
		p.AnchorSetAt = &t
	}
	return p, nil
}

// TrackPointsBetween returns the points with from <= time <= to, oldest first.
func (s *passageStore) TrackPointsBetween(from, to time.Time) ([]recordedTrackPoint, error) {
	return s.trackPoints(from, to, "")
}

// trackPoints reads a time range, optionally only one source.
func (s *passageStore) trackPoints(from, to time.Time, source string) ([]recordedTrackPoint, error) {
	rows, err := s.db.Query(`SELECT `+trackPointColumns+` FROM track_points WHERE t_ms >= ? AND t_ms <= ? AND (? = '' OR source = ?) ORDER BY t_ms, id`,
		from.UnixMilli(), to.UnixMilli(), source, source)
	if err != nil {
		return nil, fmt.Errorf("query track points: %w", err)
	}
	defer rows.Close()
	var out []recordedTrackPoint
	for rows.Next() {
		p, err := scanTrackPoint(rows)
		if err != nil {
			return nil, fmt.Errorf("scan track point: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// TrackPointsSince returns the recorded points at or after from, oldest
// first. Imported points are left out: this feeds the live detector.
func (s *passageStore) TrackPointsSince(from time.Time) ([]recordedTrackPoint, error) {
	return s.trackPoints(from, time.UnixMilli(1<<62-1), trackSourceRecorded)
}

// LastTrackPoint returns the newest point, or nil for an empty log.
func (s *passageStore) LastTrackPoint() (*recordedTrackPoint, error) {
	row := s.db.QueryRow(`SELECT ` + trackPointColumns + ` FROM track_points WHERE source = 'recorded' ORDER BY t_ms DESC, id DESC LIMIT 1`)
	p, err := scanTrackPoint(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last track point: %w", err)
	}
	return &p, nil
}

// FirstTrackPointTime returns the oldest point's time, or false for an empty log.
func (s *passageStore) FirstTrackPointTime() (time.Time, bool, error) {
	var ms sql.NullInt64
	if err := s.db.QueryRow(`SELECT MIN(t_ms) FROM track_points WHERE source = 'recorded'`).Scan(&ms); err != nil {
		return time.Time{}, false, fmt.Errorf("first track point: %w", err)
	}
	if !ms.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(ms.Int64).UTC(), true, nil
}

// DetectorResume returns where the next detection run restarts, or false
// before the first run.
func (s *passageStore) DetectorResume() (time.Time, bool, error) {
	var ms int64
	err := s.db.QueryRow(`SELECT resume_ms FROM passage_detector_state WHERE id = 1`).Scan(&ms)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("detector state: %w", err)
	}
	return time.UnixMilli(ms).UTC(), true, nil
}

func (s *passageStore) SetDetectorResume(t time.Time) error {
	_, err := s.db.Exec(`INSERT INTO passage_detector_state (id, resume_ms) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET resume_ms = excluded.resume_ms`, t.UnixMilli())
	if err != nil {
		return fmt.Errorf("save detector state: %w", err)
	}
	return nil
}

// PassageOverlaps reports whether any stored passage, hidden or not, shares
// time with [start, end]. That is how the detector avoids recreating a
// passage the operator hid or edited, and avoids doubling an existing one.
func (s *passageStore) PassageOverlaps(start, end time.Time) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM passages WHERE start_ms < ? AND end_ms > ?`,
		end.UnixMilli(), start.UnixMilli()).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("passage overlap: %w", err)
	}
	return n > 0, nil
}

// InsertPassage stores a passage and returns it with its new id.
func (s *passageStore) InsertPassage(r passageRecord) (passageRecord, error) {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO passages
		(id, start_ms, end_ms, from_name, to_name, title, notes, hidden, edited, cruise_id, created_ms)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Start.UnixMilli(), r.End.UnixMilli(), r.FromName, r.ToName, r.Title, r.Notes,
		boolInt(r.Hidden), boolInt(r.Edited), r.CruiseID, r.CreatedAt.UnixMilli())
	if err != nil {
		return r, fmt.Errorf("insert passage: %w", err)
	}
	return r, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const passageColumns = `id, start_ms, end_ms, from_name, to_name, title, notes, hidden, edited, cruise_id, created_ms`

func scanPassage(r rowScanner) (passageRecord, error) {
	var p passageRecord
	var startMs, endMs, createdMs int64
	var hidden, edited int
	var cruise sql.NullString
	if err := r.Scan(&p.ID, &startMs, &endMs, &p.FromName, &p.ToName, &p.Title, &p.Notes, &hidden, &edited, &cruise, &createdMs); err != nil {
		return p, err
	}
	p.Start, p.End = time.UnixMilli(startMs).UTC(), time.UnixMilli(endMs).UTC()
	p.Hidden, p.Edited = hidden != 0, edited != 0
	if cruise.Valid {
		c := cruise.String
		p.CruiseID = &c
	}
	p.CreatedAt = time.UnixMilli(createdMs).UTC()
	return p, nil
}

// ListPassages returns the visible (not hidden) passages, newest first.
func (s *passageStore) ListPassages() ([]passageRecord, error) {
	rows, err := s.db.Query(`SELECT ` + passageColumns + ` FROM passages WHERE hidden = 0 ORDER BY start_ms DESC`)
	if err != nil {
		return nil, fmt.Errorf("list passages: %w", err)
	}
	defer rows.Close()
	var out []passageRecord
	for rows.Next() {
		p, err := scanPassage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan passage: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPassage returns one passage by id, hidden or not.
func (s *passageStore) GetPassage(id string) (passageRecord, error) {
	p, err := scanPassage(s.db.QueryRow(`SELECT `+passageColumns+` FROM passages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, errPassageNotFound
	}
	if err != nil {
		return p, fmt.Errorf("get passage: %w", err)
	}
	return p, nil
}

func (s *passageStore) InsertCruise(c cruiseRecord) (cruiseRecord, error) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.Exec(`INSERT INTO cruises (id, name, notes, created_ms) VALUES (?,?,?,?)`,
		c.ID, c.Name, c.Notes, c.CreatedAt.UnixMilli()); err != nil {
		return c, fmt.Errorf("insert cruise: %w", err)
	}
	return c, nil
}
