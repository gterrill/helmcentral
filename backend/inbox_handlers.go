package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// HTTP API for the Inbox (ADR 0279). Reads are tierRead; the upload, bin and
// filing calls are tierWrite. Anyone with write access can share, and an item
// does not record who sent it.

const (
	inboxGroupPassage    = "passage"
	inboxGroupUnderway   = "underway"
	inboxGroupOffPassage = "off_passage"
	inboxGroupNoTime     = "no_time"

	// inboxMismatchMeters is how far the phone's own position may sit from the
	// boat's track at capture time before the item is flagged (1 nm).
	inboxMismatchMeters = 1852.0
	// inboxTrackGap is how stale the track point used for a capture time may be.
	inboxTrackGap = passageBackdateMaxGap

	inboxMaxBatch = 500
)

// inboxPlaceLookup names the place at a position. A var so tests can stand in.
var inboxPlaceLookup = func(lat, lon float64) string {
	return resolveAndCachePlaceName(placeNameProviderResolve, lat, lon)
}

// inboxCreateSign makes the sign for a filed photo. A var so a test can make
// the step fail after the photo has been copied into the library.
var inboxCreateSign = func(s *passageStore, in replaySign) (replaySign, error) { return s.CreateSign(in) }

var inboxUploadMIME = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "application/pdf": true,
}

// ── JSON shapes ──────────────────────────────────────────────────────────

type inboxGroupJSON struct {
	// Kind is passage, underway, off_passage or no_time.
	Kind      string  `json:"kind"`
	PassageID *string `json:"passage_id,omitempty"`
}

type inboxItemJSON struct {
	ID               string         `json:"id"`
	Filename         string         `json:"filename"`
	MIME             string         `json:"mime"`
	SizeBytes        int64          `json:"size_bytes"`
	CaptureMs        *int64         `json:"capture_ms"`
	CaptureOffsetMin *int           `json:"capture_offset_min"`
	BearingDeg       *float64       `json:"bearing_deg"`
	HasBearing       bool           `json:"has_bearing"`
	Fingerprint      string         `json:"fingerprint"`
	Sharpness        float64        `json:"sharpness"`
	CreatedMs        int64          `json:"created_ms"`
	ContentURL       string         `json:"content_url"`
	Group            inboxGroupJSON `json:"group"`
	// GPSMismatch says the phone's own position was more than a nautical mile
	// from the boat's track at the capture time: the clock or zone is wrong.
	GPSMismatch bool `json:"gps_mismatch"`
}

func toInboxItemJSON(it inboxItem, group inboxGroupJSON, mismatch bool) inboxItemJSON {
	return inboxItemJSON{
		ID: it.ID, Filename: it.Filename, MIME: it.MIME, SizeBytes: it.SizeBytes, CaptureMs: it.CaptureMs,
		CaptureOffsetMin: it.CaptureOffsetMin, BearingDeg: it.BearingDeg, HasBearing: it.BearingDeg != nil,
		Fingerprint: it.Fingerprint, Sharpness: it.Sharpness, CreatedMs: it.CreatedMs,
		ContentURL: "/api/inbox/" + it.ID + "/content", Group: group, GPSMismatch: mismatch,
	}
}

func inboxStoreOrError(c echo.Context) (*inboxStore, bool) {
	if globalInboxStore == nil {
		_ = c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "the Inbox is not running"})
		return nil, false
	}
	return globalInboxStore, true
}

func inboxError(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]string{"error": msg})
}

// ── the track at a capture time ──────────────────────────────────────────

// inboxTrackAt is the last track point at or before tMs, no older than
// inboxTrackGap, or nil when the boat recorded nothing near then.
func inboxTrackAt(ps *passageStore, tMs int64) (*recordedTrackPoint, error) {
	p, err := scanTrackPoint(ps.db.QueryRow(
		`SELECT `+trackPointColumns+` FROM track_points WHERE t_ms <= ? AND t_ms >= ? ORDER BY t_ms DESC, id DESC LIMIT 1`,
		tMs, tMs-inboxTrackGap.Milliseconds()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// lastPassageEndMs is the end of the latest passage, hidden or not, or 0 with
// none. Filing accepts a time inside a hidden passage, so grouping counts it too.
func lastPassageEndMs(ps *passageStore) (int64, error) {
	var end sql.NullInt64
	if err := ps.db.QueryRow(`SELECT MAX(end_ms) FROM passages`).Scan(&end); err != nil {
		return 0, err
	}
	return end.Int64, nil
}

// ── POST /api/inbox ──────────────────────────────────────────────────────

var inboxHexRE = regexp.MustCompile(`^[0-9a-f]{0,64}$`)

// inboxMeta is what the phone sends beside the file, already checked.
type inboxMeta struct {
	captureMs        *int64
	captureOffsetMin *int
	bearingDeg       *float64
	phoneLat         *float64
	phoneLon         *float64
	fingerprint      string
	sharpness        float64
	sourceSHA256     string
}

var inboxSHA256RE =regexp.MustCompile(`^[0-9a-f]{64}$`)

func parseInboxMeta(f map[string]string) (inboxMeta, error) {
	var m inboxMeta
	num := func(name string) (*float64, error) {
		raw := strings.TrimSpace(f[name])
		if raw == "" {
			return nil, nil
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%s must be a number", name)
		}
		return &v, nil
	}
	if raw := strings.TrimSpace(f["capture_ms"]); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			return m, errors.New("capture_ms must be a Unix time in milliseconds")
		}
		m.captureMs = &v
	}
	if raw := strings.TrimSpace(f["capture_offset_min"]); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < -14*60 || v > 14*60 {
			return m, errors.New("capture_offset_min must be minutes between -840 and 840")
		}
		m.captureOffsetMin = &v
	}
	var err error
	if m.bearingDeg, err = num("bearing_deg"); err != nil {
		return m, err
	}
	if m.bearingDeg != nil && (*m.bearingDeg < 0 || *m.bearingDeg >= 360) {
		return m, errors.New("bearing_deg must be from 0 up to 360")
	}
	lat, err := num("phone_lat")
	if err != nil {
		return m, err
	}
	lon, err := num("phone_lon")
	if err != nil {
		return m, err
	}
	if (lat == nil) != (lon == nil) {
		return m, errors.New("phone_lat and phone_lon come together")
	}
	if lat != nil {
		if *lat < -90 || *lat > 90 || *lon < -180 || *lon > 180 {
			return m, errors.New("phone_lat or phone_lon is out of range")
		}
		// A phone that hides its location writes zeros, which is no position.
		if !(*lat == 0 && *lon == 0) {
			m.phoneLat, m.phoneLon = lat, lon
		}
	}
	sharp, err := num("sharpness")
	if err != nil {
		return m, err
	}
	if sharp != nil {
		if *sharp < 0 {
			return m, errors.New("sharpness cannot be negative")
		}
		m.sharpness = *sharp
	}
	if raw := strings.TrimSpace(f["source_sha256"]); raw != "" {
		if !inboxSHA256RE.MatchString(raw) {
			return m, errors.New("source_sha256 must be 64 lowercase hex characters")
		}
		m.sourceSHA256 = raw
	}
	m.fingerprint = strings.TrimSpace(f["fingerprint"])
	if !inboxHexRE.MatchString(m.fingerprint) {
		return m, errors.New("fingerprint must be hex, at most 64 characters")
	}
	return m, nil
}

var inboxFieldNames = map[string]bool{
	"capture_ms": true, "capture_offset_min": true, "bearing_deg": true, "phone_lat": true,
	"phone_lon": true, "fingerprint": true, "sharpness": true, "source_sha256": true,
}

// uploadInboxHandler takes one file per request. A file whose bytes are
// already in the Inbox or the library answers 200 "already_uploaded" so the
// phone can count the skip; a new one answers 201 "stored".
func uploadInboxHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	docs, err := requireDocumentStore("inbox upload")
	if err != nil {
		return inboxError(c, http.StatusServiceUnavailable, err.Error())
	}
	dir := inboxDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("inbox: create directory: %v", err)
		return inboxError(c, http.StatusInternalServerError, "failed to prepare Inbox storage")
	}

	fields := map[string]string{}
	up, err := receiveUploadedFile(c, dir, func(name string, part *multipart.Part) error {
		defer part.Close()
		if !inboxFieldNames[name] {
			return nil
		}
		b, rerr := io.ReadAll(io.LimitReader(part, 256))
		if rerr != nil {
			return rerr
		}
		fields[name] = string(b)
		return nil
	})
	if err != nil {
		return err
	}
	meta, err := parseInboxMeta(fields)
	if err != nil {
		os.Remove(up.tmpPath)
		return inboxError(c, http.StatusBadRequest, err.Error())
	}
	mimeType := detectDocumentMIME(up.head, up.filename)
	if !inboxUploadMIME[mimeType] {
		os.Remove(up.tmpPath)
		return inboxError(c, http.StatusUnsupportedMediaType, "only photos (JPEG, PNG, GIF, WebP) and PDFs can be shared to the Inbox; this file reads as "+mimeType)
	}

	unlock := lockDocumentSHA(up.sha)
	defer unlock()
	// The same original, however it was shrunk, is not a new photo.
	if meta.sourceSHA256 != "" {
		inInbox, err := store.HasSource(meta.sourceSHA256)
		if err == nil && inInbox {
			os.Remove(up.tmpPath)
			return c.JSON(http.StatusOK, map[string]any{"status": "already_uploaded", "where": "inbox"})
		}
		filed := false
		if err == nil {
			filed, err = store.SourceFiled(meta.sourceSHA256)
		}
		if err != nil {
			os.Remove(up.tmpPath)
			log.Printf("inbox: %v", err)
			return inboxError(c, http.StatusInternalServerError, "could not check the Inbox")
		}
		if filed {
			os.Remove(up.tmpPath)
			return c.JSON(http.StatusOK, map[string]any{"status": "already_uploaded", "where": "filed"})
		}
	}
	if _, found, err := store.GetBySHA(up.sha); err != nil {
		os.Remove(up.tmpPath)
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "could not check the Inbox")
	} else if found {
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusOK, map[string]any{"status": "already_uploaded", "where": "inbox"})
	}
	if _, found, err := docs.GetBySHA(up.sha); err != nil {
		os.Remove(up.tmpPath)
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "could not check Documents")
	} else if found {
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusOK, map[string]any{"status": "already_uploaded", "where": "documents"})
	}

	final := inboxFilePath(up.sha)
	if err := os.Rename(up.tmpPath, final); err != nil {
		os.Remove(up.tmpPath)
		log.Printf("inbox: store upload: %v", err)
		return inboxError(c, http.StatusInternalServerError, "failed to store upload")
	}
	item, err := store.Insert(inboxItem{
		SHA256: up.sha, Filename: filepathBase(up.filename), MIME: mimeType, SizeBytes: up.size,
		CaptureMs: meta.captureMs, CaptureOffsetMin: meta.captureOffsetMin, BearingDeg: meta.bearingDeg,
		PhoneLat: meta.phoneLat, PhoneLon: meta.phoneLon, Fingerprint: meta.fingerprint, Sharpness: meta.sharpness,
		SourceSHA256: meta.sourceSHA256,
	})
	if errors.Is(err, errInboxDuplicate) {
		// Lost a race to an identical upload: its file is the one in place.
		return c.JSON(http.StatusOK, map[string]any{"status": "already_uploaded", "where": "inbox"})
	}
	if err != nil {
		os.Remove(final)
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "failed to record the upload")
	}
	return c.JSON(http.StatusCreated, map[string]any{
		"status": "stored",
		"item":   toInboxItemJSON(item, inboxGroupJSON{Kind: inboxGroupNoTime}, false),
	})
}

// ── GET /api/inbox ───────────────────────────────────────────────────────

// listInboxHandler lists the items in capture order, each with the group the
// operator will see it under:
//
//   - no_time: the file carried no capture time.
//   - passage: the capture time falls inside a visible passage (the latest
//     start wins if two overlap).
//   - underway: after the end of the last recorded passage, hidden ones
//     included since filing accepts them (or with none recorded) and the boat was not at rest at that moment, per the track.
//     The passage is only recorded once it closes, so these cannot be assigned
//     yet; they move into the passage when it is recorded.
//   - off_passage: everything else with a time.
//
// gps_mismatch is set when the phone's own position, if it sent a real one,
// was over a nautical mile from the track at the capture time.
func listInboxHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	ps, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	items, err := store.List()
	if err != nil {
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "could not list the Inbox")
	}
	passages, err := ps.ListPassages()
	if err != nil {
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "could not read the passages")
	}
	lastEnd, err := lastPassageEndMs(ps)
	if err != nil {
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "could not read the passages")
	}
	out := make([]inboxItemJSON, 0, len(items))
	for _, it := range items {
		group := inboxGroupJSON{Kind: inboxGroupNoTime}
		mismatch := false
		if it.CaptureMs != nil {
			t := *it.CaptureMs
			pt, err := inboxTrackAt(ps, t)
			if err != nil {
				log.Printf("inbox: %v", err)
				return inboxError(c, http.StatusInternalServerError, "could not read the track")
			}
			group = inboxGroupJSON{Kind: inboxGroupOffPassage}
			for _, p := range passages { // newest start first
				if p.Start.UnixMilli() <= t && t <= p.End.UnixMilli() {
					id := p.ID
					group = inboxGroupJSON{Kind: inboxGroupPassage, PassageID: &id}
					break
				}
			}
			if group.Kind == inboxGroupOffPassage && t > lastEnd && pt != nil && pt.SOGKts != nil && *pt.SOGKts >= passageRestSOGKts {
				group = inboxGroupJSON{Kind: inboxGroupUnderway}
			}
			if pt != nil && it.PhoneLat != nil && it.PhoneLon != nil {
				mismatch = haversineMeters(*it.PhoneLat, *it.PhoneLon, pt.Lat, pt.Lon) > inboxMismatchMeters
			}
		}
		out = append(out, toInboxItemJSON(it, group, mismatch))
	}
	return c.JSON(http.StatusOK, map[string]any{"items": out})
}

// inboxCountHandler is GET /api/inbox/count, cheap enough for a badge.
func inboxCountHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	n, err := store.Count()
	if err != nil {
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "could not count the Inbox")
	}
	return c.JSON(http.StatusOK, map[string]int{"count": n})
}

// inboxContentHandler is GET /api/inbox/:id/content.
func inboxContentHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	it, err := store.Get(c.Param("id"))
	if errors.Is(err, errInboxItemNotFound) {
		return inboxError(c, http.StatusNotFound, err.Error())
	}
	if err != nil {
		log.Printf("inbox: %v", err)
		return inboxError(c, http.StatusInternalServerError, "could not read the item")
	}
	f, err := os.Open(inboxFilePath(it.SHA256))
	if err != nil {
		log.Printf("inbox: open %s: %v", it.ID, err)
		return inboxError(c, http.StatusInternalServerError, "the file is missing from Inbox storage")
	}
	defer f.Close()
	h := c.Response().Header()
	h.Set(echo.HeaderContentType, it.MIME)
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Response(), c.Request(), it.Filename, time.UnixMilli(it.CreatedMs), f)
	return nil
}

// ── POST /api/inbox/bin ──────────────────────────────────────────────────

// removeInboxItem deletes the row, then the file, under the content's lock.
func removeInboxItem(store *inboxStore, it inboxItem) error {
	unlock := lockDocumentSHA(it.SHA256)
	defer unlock()
	if err := store.Delete(it.ID); err != nil {
		return err
	}
	if err := os.Remove(inboxFilePath(it.SHA256)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove inbox file: %w", err)
	}
	return nil
}

// binInboxHandler deletes the given items outright. Ids already gone are
// skipped, so a repeated tap is harmless.
func binInboxHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if len(body.IDs) == 0 || len(body.IDs) > inboxMaxBatch {
		return inboxError(c, http.StatusBadRequest, fmt.Sprintf("ids must hold 1 to %d items", inboxMaxBatch))
	}
	deleted := 0
	for _, id := range body.IDs {
		it, err := store.Get(id)
		if errors.Is(err, errInboxItemNotFound) {
			continue
		}
		if err == nil {
			err = removeInboxItem(store, it)
		}
		if errors.Is(err, errInboxItemNotFound) {
			continue
		}
		if err != nil {
			log.Printf("inbox: bin %s: %v", id, err)
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "could not remove every item", "deleted": deleted})
		}
		deleted++
	}
	return c.JSON(http.StatusOK, map[string]int{"deleted": deleted})
}

// ── filing ───────────────────────────────────────────────────────────────

type inboxResult struct {
	ID         string `json:"id"`
	OK         bool   `json:"ok"`
	SignID     string `json:"sign_id,omitempty"`
	DocumentID string `json:"document_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// inboxFail is a result for an item that stays in the Inbox. A refusal the
// operator can act on keeps its words; anything else is logged and kept short.
func inboxFail(id string, err error) inboxResult {
	var refusal inboxRefusal
	var msg string
	switch {
	case errors.As(err, &refusal), errors.Is(err, errPassageInvalid), errors.Is(err, errInboxItemNotFound), errors.Is(err, errFolderNotFound):
		msg = err.Error()
	default:
		// The detail (paths, database errors) goes to the log, not the screen.
		log.Printf("inbox: filing %s: %v", id, err)
		msg = "could not file this item; the Helmcentral log says why"
	}
	return inboxResult{ID: id, Error: msg}
}

// inboxClaims holds the ids being filed right now, so a double tap or a retry
// that overlaps cannot make two signs from one photo.
var inboxClaims sync.Map

func claimInboxItem(id string) (release func(), err error) {
	if _, busy := inboxClaims.LoadOrStore(id, struct{}{}); busy {
		return nil, refusedf("this item is already being filed")
	}
	return func() { inboxClaims.Delete(id) }, nil
}

// inboxRemoveItem clears a filed item from the Inbox. A var so a test can make it fail.
var inboxRemoveItem = removeInboxItem

// signCount is how many signs show this library document.
func signCountForDocument(ps *passageStore, docID string) (int, error) {
	var n int
	err := ps.db.QueryRow(`SELECT COUNT(*) FROM replay_signs WHERE document_id = ?`, docID).Scan(&n)
	return n, err
}

// inboxRefusal is a plain-words reason an item cannot be filed as asked.
type inboxRefusal struct{ msg string }

func (e inboxRefusal) Error() string { return e.msg }

func refusedf(format string, a ...any) error { return inboxRefusal{fmt.Sprintf(format, a...)} }

var passagePhotosFolderMu sync.Mutex

const passagePhotosFolderName = "Passage photos"

// passagePhotosFolderID finds the root folder the sign editor also uses (by
// name, ignoring capitals) or makes it.
func passagePhotosFolderID(docs *documentStore) (string, error) {
	passagePhotosFolderMu.Lock()
	defer passagePhotosFolderMu.Unlock()
	find := func() (string, error) {
		folders, _, err := docs.ListFolder(nil)
		if err != nil {
			return "", err
		}
		for _, f := range folders {
			if strings.EqualFold(f.Name, passagePhotosFolderName) {
				return f.ID, nil
			}
		}
		return "", nil
	}
	if id, err := find(); err != nil || id != "" {
		return id, err
	}
	f, err := docs.CreateFolder(passagePhotosFolderName, nil)
	if errors.Is(err, errFolderNameTaken) {
		if id, ferr := find(); ferr != nil || id != "" {
			return id, ferr
		}
	}
	if err != nil {
		return "", err
	}
	return f.ID, nil
}

// inboxCopyToLibrary makes a Document of an item's file, through the same
// store-and-dedupe step an upload takes, so the indexer picks it up. created
// is false when the library already held these bytes, in which case existing
// is that document.
func inboxCopyToLibrary(it inboxItem, folderID *string, title string) (doc document, created bool, err error) {
	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return document{}, false, fmt.Errorf("prepare document storage: %w", err)
	}
	enrich, _, err := documentEnrichFlag("inbox: file")
	if err != nil {
		return document{}, false, err
	}
	src, err := os.Open(inboxFilePath(it.SHA256))
	if err != nil {
		return document{}, false, fmt.Errorf("the file is missing from Inbox storage: %w", err)
	}
	defer src.Close()
	tmp, err := os.CreateTemp(dir, "upload-*.tmp")
	if err != nil {
		return document{}, false, fmt.Errorf("create temp file: %w", err)
	}
	head := &headCapture{}
	n, copyErr := io.Copy(io.MultiWriter(tmp, head), src)
	if closeErr := tmp.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		os.Remove(tmp.Name())
		return document{}, false, fmt.Errorf("copy to library: %w", copyErr)
	}
	up := uploadedFile{tmpPath: tmp.Name(), filename: it.Filename, size: n, sha: it.SHA256, head: head.buf}
	candidate := document{FolderID: folderID, Filename: it.Filename, Title: title, MIME: it.MIME, SizeBytes: n, Enrich: enrich}
	err = storeUploadedFile(dir, up, candidate,
		func(existing document) error { doc, created = existing, false; return nil },
		func(inserted document) error { doc, created = inserted, true; return nil },
		func(err error) error { return err },
	)
	return doc, created, err
}

// inboxDiscardDocument undoes inboxCopyToLibrary for a document it just made.
func inboxDiscardDocument(docs *documentStore, doc document) {
	unlock := lockDocumentSHA(doc.SHA256)
	defer unlock()
	if _, err := docs.Delete(doc.ID); err != nil {
		log.Printf("inbox: undo library copy %s: %v", doc.ID, err)
		return
	}
	if err := os.Remove(filepath.Join(documentsDirPath(), doc.SHA256)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("inbox: undo library copy file %s: %v", doc.ID, err)
	}
}

type inboxPassageRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// TMs overrides the capture time, for a photo taken between passages that
	// the operator attaches to a passage's start or end, or one with no time.
	TMs *int64 `json:"t_ms"`
}

func fileInboxItemToPassage(store *inboxStore, ps *passageStore, docs *documentStore, req inboxPassageRequest) (inboxResult, error) {
	release, err := claimInboxItem(req.ID)
	if err != nil {
		return inboxResult{}, err
	}
	defer release()
	// Read after the claim, so an item another request just filed is gone.
	it, err := store.Get(req.ID)
	if err != nil {
		return inboxResult{}, err
	}
	title := strings.TrimSpace(req.Title)
	if n := utf8.RuneCountInString(title); n == 0 || n > signTitleMax {
		return inboxResult{}, refusedf("a title of 1 to %d characters is required", signTitleMax)
	}
	if !signPhotoMIME[it.MIME] {
		return inboxResult{}, refusedf("only photos can be signs; this is a %s", it.MIME)
	}
	var tMs int64
	switch {
	case req.TMs != nil:
		tMs = *req.TMs
	case it.CaptureMs != nil:
		tMs = *it.CaptureMs
	default:
		return inboxResult{}, refusedf("this photo has no capture time; choose a time on a passage")
	}
	if err := timeInsideAPassage(ps.db, tMs); err != nil {
		return inboxResult{}, refusedf("that time is not inside any recorded passage")
	}
	folder, err := passagePhotosFolderID(docs)
	if err != nil {
		return inboxResult{}, err
	}
	doc, created, err := inboxCopyToLibrary(it, &folder, "")
	if err != nil {
		return inboxResult{}, err
	}
	if !created {
		// The library already held this photo. If a sign shows it, an earlier
		// filing got as far as the sign and failed to clear the item: finish
		// that, rather than make a second sign.
		n, cerr := signCountForDocument(ps, doc.ID)
		if cerr != nil {
			return inboxResult{}, cerr
		}
		if n > 0 {
			if rerr := inboxRemoveItem(store, it); rerr != nil {
				log.Printf("inbox: clearing %s: %v", it.ID, rerr)
				return inboxResult{}, refusedf("a sign already uses this photo, but it could not be cleared from the Inbox; delete it there")
			}
			if merr := store.MarkFiled(it.SourceSHA256); merr != nil {
				log.Printf("inbox: %v", merr)
			}
			return inboxResult{}, refusedf("a sign already uses this photo, so it was cleared from the Inbox")
		}
	}
	moment := true
	sign, err := inboxCreateSign(ps, replaySign{TMs: tMs, Title: title, DocumentID: &doc.ID, Visibility: signPassing, ShowS: signShowDef, PhotoMoment: &moment})
	if err != nil {
		if created {
			inboxDiscardDocument(docs, doc)
		}
		return inboxResult{}, err
	}
	// Remember the original before clearing, so a failure to clear still
	// leaves the photo recognised if it is shared again.
	if merr := store.MarkFiled(it.SourceSHA256); merr != nil {
		log.Printf("inbox: %v", merr)
	}
	if err := inboxRemoveItem(store, it); err != nil {
		log.Printf("inbox: clearing %s after its sign: %v", it.ID, err)
		return inboxResult{}, refusedf("the sign was made, but the item could not be cleared from the Inbox; delete it there")
	}
	wakeDocumentIndexer()
	return inboxResult{ID: it.ID, OK: true, SignID: sign.ID, DocumentID: doc.ID}, nil
}

// fileInboxToPassageHandler is POST /api/inbox/file-to-passage. Each item is
// filed on its own: a failure leaves that item in the Inbox and is reported
// against its id, and the others still go through.
func fileInboxToPassageHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	ps, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	docs, err := requireDocumentStore("inbox filing")
	if err != nil {
		return inboxError(c, http.StatusServiceUnavailable, err.Error())
	}
	var body struct {
		Items []inboxPassageRequest `json:"items"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if len(body.Items) == 0 || len(body.Items) > inboxMaxBatch {
		return inboxError(c, http.StatusBadRequest, fmt.Sprintf("items must hold 1 to %d entries", inboxMaxBatch))
	}
	results := make([]inboxResult, 0, len(body.Items))
	for _, req := range body.Items {
		res, err := fileInboxItemToPassage(store, ps, docs, req)
		if err != nil {
			res = inboxFail(req.ID, err)
		}
		results = append(results, res)
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}

type inboxDocumentRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func fileInboxItemToDocuments(store *inboxStore, folderID string, req inboxDocumentRequest) (inboxResult, error) {
	release, err := claimInboxItem(req.ID)
	if err != nil {
		return inboxResult{}, err
	}
	defer release()
	it, err := store.Get(req.ID)
	if err != nil {
		return inboxResult{}, err
	}
	doc, created, err := inboxCopyToLibrary(it, &folderID, strings.TrimSpace(req.Title))
	if err != nil {
		return inboxResult{}, err
	}
	if !created {
		return inboxResult{}, refusedf("these bytes are already in Documents as %q; bin this copy", firstNonEmpty(doc.Title, doc.Filename))
	}
	if merr := store.MarkFiled(it.SourceSHA256); merr != nil {
		log.Printf("inbox: %v", merr)
	}
	if err := inboxRemoveItem(store, it); err != nil {
		log.Printf("inbox: clearing %s after its document: %v", it.ID, err)
		return inboxResult{}, refusedf("the document was made, but the item could not be cleared from the Inbox; delete it there")
	}
	wakeDocumentIndexer()
	return inboxResult{ID: it.ID, OK: true, DocumentID: doc.ID}, nil
}

// fileInboxToDocumentsHandler is POST /api/inbox/file-to-documents.
func fileInboxToDocumentsHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	if _, err := requireDocumentStore("inbox filing"); err != nil {
		return inboxError(c, http.StatusServiceUnavailable, err.Error())
	}
	var body struct {
		FolderID string                `json:"folder_id"`
		Items    []inboxDocumentRequest `json:"items"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if strings.TrimSpace(body.FolderID) == "" {
		return inboxError(c, http.StatusBadRequest, "folder_id is required")
	}
	if len(body.Items) == 0 || len(body.Items) > inboxMaxBatch {
		return inboxError(c, http.StatusBadRequest, fmt.Sprintf("items must hold 1 to %d entries", inboxMaxBatch))
	}
	results := make([]inboxResult, 0, len(body.Items))
	for _, req := range body.Items {
		res, err := fileInboxItemToDocuments(store, body.FolderID, req)
		if err != nil {
			res = inboxFail(req.ID, err)
		}
		results = append(results, res)
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}

// ── GET /api/inbox/suggested-titles ──────────────────────────────────────

// inboxSuggestedTitlesHandler answers ?ids=a,b with the nearest named place
// to the boat at each item's capture time, or "" when the item has no time,
// the track has nothing near it, or nothing is named there.
func inboxSuggestedTitlesHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	ps, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	ids := strings.Split(c.QueryParam("ids"), ",")
	titles := map[string]string{}
	type lookup = inboxLookup
	var lookups []lookup
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if len(titles) >= inboxMaxBatch {
			return inboxError(c, http.StatusBadRequest, fmt.Sprintf("at most %d ids", inboxMaxBatch))
		}
		it, err := store.Get(id)
		if errors.Is(err, errInboxItemNotFound) {
			titles[id] = ""
			continue
		}
		if err != nil {
			log.Printf("inbox: %v", err)
			return inboxError(c, http.StatusInternalServerError, "could not read the item")
		}
		titles[id] = ""
		if it.CaptureMs == nil {
			continue
		}
		pt, err := inboxTrackAt(ps, *it.CaptureMs)
		if err != nil {
			log.Printf("inbox: %v", err)
			return inboxError(c, http.StatusInternalServerError, "could not read the track")
		}
		if pt == nil {
			continue
		}
		lookups = append(lookups, lookup{id, pt.Lat, pt.Lon})
	}

	// The place lookups can be slow (a public Overpass server may time out), so
	// run a few at once and stop waiting at the deadline. A title still
	// outstanding then is left blank for the operator to type; the request does
	// not fail.
	lookupName, deadline := inboxPlaceLookup, inboxTitleDeadline
	var mu sync.Mutex
	found := map[string]string{}
	work := make(chan lookup)
	var wg sync.WaitGroup
	for i := 0; i < inboxTitleWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for l := range work {
				name := joinTitle(lookupName(l.lat, l.lon))
				mu.Lock()
				found[l.id] = name
				mu.Unlock()
			}
		}()
	}
	go func() {
		for _, l := range lookups {
			work <- l
		}
		close(work)
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(deadline):
		log.Printf("inbox: place names not all found within %s; the rest are left blank", deadline)
	}
	mu.Lock()
	for id, name := range found {
		titles[id] = name
	}
	mu.Unlock()
	return c.JSON(http.StatusOK, map[string]any{"titles": titles})
}

type inboxLookup struct {
	id       string
	lat, lon float64
}

const inboxTitleWorkers = 4

// inboxTitleDeadline is how long the title request waits for place names. A var for tests.
var inboxTitleDeadline = 8 * time.Second

// inboxRoutes returns the /api routes for the Inbox, each with its auth tier.
func inboxRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/inbox", tierRead, listInboxHandler},
		{http.MethodGet, "/api/inbox/count", tierRead, inboxCountHandler},
		{http.MethodGet, "/api/inbox/suggested-titles", tierRead, inboxSuggestedTitlesHandler},
		{http.MethodGet, "/api/inbox/:id/content", tierRead, inboxContentHandler},
		{http.MethodPost, "/api/inbox", tierWrite, uploadInboxHandler},
		{http.MethodPost, "/api/inbox/bin", tierWrite, binInboxHandler},
		{http.MethodPost, "/api/inbox/file-to-passage", tierWrite, fileInboxToPassageHandler},
		{http.MethodPost, "/api/inbox/file-to-documents", tierWrite, fileInboxToDocumentsHandler},
	}
}
