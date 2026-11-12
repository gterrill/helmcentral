package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

	// inboxThumbMaxBytes caps the preview the phone sends beside a photo (it
	// makes a JPEG about 400 px across, a few tens of kilobytes).
	inboxThumbMaxBytes = 512 << 10

	passagePhotosFolderName = "Passage photos"
)

// inboxPlaceLookup names the place at a position. A var so tests can stand in.
var inboxPlaceLookup = func(lat, lon float64) string {
	return resolveAndCachePlaceName(placeNameProviderResolve, lat, lon)
}

// inboxCreateSign makes the sign for a filed photo. A var so a test can make
// the step fail after the photo has been copied into the library.
var inboxCreateSign = func(s *passageStore, in replaySign) (replaySign, error) { return s.CreateSign(in) }

// inboxUploadMIME is what the Inbox takes: the photos a sign can show, and PDFs.
var inboxUploadMIME = func() map[string]bool {
	m := map[string]bool{"application/pdf": true}
	for mime := range signPhotoMIME {
		m[mime] = true
	}
	return m
}()

// ── JSON shapes ──────────────────────────────────────────────────────────

type inboxGroupJSON struct {
	// Kind is passage, underway, off_passage or no_time.
	Kind      string  `json:"kind"`
	PassageID *string `json:"passage_id,omitempty"`
}

type inboxItemJSON struct {
	ID          string   `json:"id"`
	Filename    string   `json:"filename"`
	MIME        string   `json:"mime"`
	SizeBytes   int64    `json:"size_bytes"`
	CaptureMs   *int64   `json:"capture_ms"`
	BearingDeg  *float64 `json:"bearing_deg"`
	Fingerprint string   `json:"fingerprint"`
	Sharpness   float64  `json:"sharpness"`
	CreatedMs   int64    `json:"created_ms"`
	ContentURL  string   `json:"content_url"`
	// ThumbURL is the small preview, absent for an item with none (a PDF).
	ThumbURL string         `json:"thumb_url,omitempty"`
	Group    inboxGroupJSON `json:"group"`
	// SuggestedKind is what Mate made of the item (scenery, document or
	// equipment), null until it has looked and for good when Mate is not set up.
	SuggestedKind *string `json:"suggested_kind"`
	// ClassifyError is the one plain line the tile shows when Mate tried and
	// failed to sort the item, with the time of the next try on the boat's clock.
	ClassifyError string `json:"classify_error,omitempty"`
	// NextTryMs is when Mate tries again, Unix ms, so the page can look again
	// then; absent when no try is scheduled.
	NextTryMs int64 `json:"next_try_ms,omitempty"`
	// GPSMismatch says the phone's own position was more than a nautical mile
	// from the boat's track at the capture time: the clock or zone is wrong.
	GPSMismatch bool `json:"gps_mismatch"`
}

// inboxNow is the clock the list judges a retry time against. A var for tests.
var inboxNow = time.Now

func toInboxItemJSON(it inboxItem, group inboxGroupJSON, mismatch bool, boat boatTime, mateOn bool) inboxItemJSON {
	out := inboxItemJSON{
		ID: it.ID, Filename: it.Filename, MIME: it.MIME, SizeBytes: it.SizeBytes, CaptureMs: it.CaptureMs,
		BearingDeg: it.BearingDeg, Fingerprint: it.Fingerprint, Sharpness: it.Sharpness, CreatedMs: it.CreatedMs,
		ContentURL: "/api/inbox/" + it.ID + "/content", Group: group, GPSMismatch: mismatch,
	}
	if it.HasThumb {
		out.ThumbURL = "/api/inbox/" + it.ID + "/thumb"
	}
	if it.SuggestedKind != "" {
		kind := it.SuggestedKind
		out.SuggestedKind = &kind
	}
	out.ClassifyError = inboxSortFailedLine(it, boat, mateOn, inboxNow())
	if it.ClassifyError != "" && it.NextTryMs > 0 && mateOn {
		out.NextTryMs = it.NextTryMs
	}
	return out
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

// inboxHTTPError is a failure with the status and words the caller should see.
// Anything else that reaches inboxRespond is logged and answered as a 500.
type inboxHTTPError struct {
	status int
	msg    string
}

func (e inboxHTTPError) Error() string { return e.msg }

func inboxRespond(c echo.Context, err error) error {
	var he inboxHTTPError
	if errors.As(err, &he) {
		return inboxError(c, he.status, he.msg)
	}
	log.Printf("inbox: %v", err)
	return inboxError(c, http.StatusInternalServerError, "the Inbox could not do that; the Helmcentral log says why")
}

// validHex says s is lowercase hex of minLen to maxLen characters.
func validHex(s string, minLen, maxLen int) bool {
	if len(s) < minLen || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if ch := s[i]; !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

// ── the track at a capture time ──────────────────────────────────────────

// inboxTrack answers "where was the boat at this time" for many times from the
// few stretches of track they fall in, read once each.
type inboxTrack struct {
	fixes []trackFix // oldest first
}

// loadInboxTrack reads the track from inboxTrackGap before the earliest time
// to the latest. Times far apart read only the stretches around them, so one
// stray old photo does not pull in months of track.
func loadInboxTrack(ps *passageStore, times []int64) (*inboxTrack, error) {
	sorted := append([]int64(nil), times...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	gap := inboxTrackGap.Milliseconds()
	tr := &inboxTrack{}
	for i := 0; i < len(sorted); {
		start, end := sorted[i]-gap, sorted[i]
		for i++; i < len(sorted) && sorted[i]-gap <= end; i++ {
			end = sorted[i]
		}
		fixes, err := ps.trackFixesIn(start, end)
		if err != nil {
			return nil, err
		}
		tr.fixes = append(tr.fixes, fixes...)
	}
	return tr, nil
}

// at is the last fix at or before tMs, no older than inboxTrackGap, or nil
// when the boat recorded nothing near then.
func (tr *inboxTrack) at(tMs int64) *trackFix {
	i := sort.Search(len(tr.fixes), func(i int) bool { return tr.fixes[i].TMs > tMs }) - 1
	if i < 0 || tr.fixes[i].TMs < tMs-inboxTrackGap.Milliseconds() {
		return nil
	}
	return &tr.fixes[i]
}

func captureTimes(items []inboxItem) []int64 {
	var out []int64
	for _, it := range items {
		if it.CaptureMs != nil {
			out = append(out, *it.CaptureMs)
		}
	}
	return out
}

// ── POST /api/inbox ──────────────────────────────────────────────────────

// inboxMeta is what the phone sends beside the file, already checked.
type inboxMeta struct {
	captureMs    *int64
	bearingDeg   *float64
	phoneLat     *float64
	phoneLon     *float64
	fingerprint  string
	sharpness    float64
	sourceSHA256 string
}

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
		if !validHex(raw, 64, 64) {
			return m, errors.New("source_sha256 must be 64 lowercase hex characters")
		}
		m.sourceSHA256 = raw
	}
	m.fingerprint = strings.TrimSpace(f["fingerprint"])
	if !validHex(m.fingerprint, 0, 64) {
		return m, errors.New("fingerprint must be hex, at most 64 characters")
	}
	return m, nil
}

var inboxFieldNames = map[string]bool{
	"capture_ms": true, "bearing_deg": true, "phone_lat": true,
	"phone_lon": true, "fingerprint": true, "sharpness": true, "source_sha256": true,
}

// looksLikeJPEG checks the three bytes every JPEG starts with.
func looksLikeJPEG(b []byte) bool {
	return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

// inboxDuplicateWhere says where these bytes are already held: "inbox" (this
// file, or the same original shrunk another way), "filed" (the original left
// the Inbox before), "documents", or "" when they are new.
func inboxDuplicateWhere(store *inboxStore, docs *documentStore, sourceSHA, sha string) (string, error) {
	if sourceSHA != "" {
		if where, err := store.SourceWhere(sourceSHA); err != nil || where != inboxWhereNone {
			return where, err
		}
	}
	if _, found, err := store.GetBySHA(sha); err != nil {
		return "", err
	} else if found {
		return inboxWhereInbox, nil
	}
	if _, found, err := docs.GetBySHA(sha); err != nil {
		return "", err
	} else if found {
		return "documents", nil
	}
	return inboxWhereNone, nil
}

// acceptInboxUpload checks a received upload and stores it. It returns the new
// item, or the place the bytes were already held. The caller removes up's temp
// file, which is gone once the bytes are stored.
func acceptInboxUpload(store *inboxStore, docs *documentStore, up uploadedFile, fields map[string]string, thumb []byte, thumbTooBig bool) (item inboxItem, where string, err error) {
	meta, err := parseInboxMeta(fields)
	if err != nil {
		return item, "", inboxHTTPError{http.StatusBadRequest, err.Error()}
	}
	if thumbTooBig || (thumb != nil && !looksLikeJPEG(thumb)) {
		return item, "", inboxHTTPError{http.StatusBadRequest, fmt.Sprintf("thumb must be a JPEG of at most %d bytes", inboxThumbMaxBytes)}
	}
	mimeType := detectDocumentMIME(up.head, up.filename)
	if !inboxUploadMIME[mimeType] {
		return item, "", inboxHTTPError{http.StatusUnsupportedMediaType, "only photos (JPEG, PNG, GIF, WebP) and PDFs can be shared to the Inbox; this file reads as " + mimeType}
	}

	// Two shrinks of one original have different bytes, so the bytes' lock alone
	// would let both through: the original's hash is locked too, in sorted order
	// so two uploads cannot wait on each other.
	locks := []string{up.sha}
	if meta.sourceSHA256 != "" && meta.sourceSHA256 != up.sha {
		locks = append(locks, meta.sourceSHA256)
		sort.Strings(locks)
	}
	for _, sha := range locks {
		defer lockDocumentSHA(sha)()
	}
	if where, err = inboxDuplicateWhere(store, docs, meta.sourceSHA256, up.sha); err != nil || where != inboxWhereNone {
		return item, where, err
	}

	final := inboxFilePath(up.sha)
	if err := os.Rename(up.tmpPath, final); err != nil {
		return item, "", fmt.Errorf("store upload: %w", err)
	}
	item, err = store.Insert(inboxItem{
		SHA256: up.sha, Filename: filepathBase(up.filename), MIME: mimeType, SizeBytes: up.size,
		CaptureMs: meta.captureMs, BearingDeg: meta.bearingDeg, PhoneLat: meta.phoneLat, PhoneLon: meta.phoneLon,
		Fingerprint: meta.fingerprint, Sharpness: meta.sharpness, SourceSHA256: meta.sourceSHA256, HasThumb: thumb != nil,
	})
	if errors.Is(err, errInboxDuplicate) {
		// Lost a race to an identical upload: its files, thumbnail included, are
		// the ones in place, and this one wrote nothing beside them.
		return item, inboxWhereInbox, nil
	}
	if err != nil {
		os.Remove(final)
		return item, "", err
	}
	// The preview is written only once the row is ours, so a duplicate never
	// touches the existing item's.
	if thumb != nil {
		if err := os.WriteFile(inboxThumbPath(up.sha), thumb, 0o644); err != nil {
			if derr := store.Delete(item.ID); derr != nil {
				log.Printf("inbox: undo %s: %v", item.ID, derr)
			}
			os.Remove(final)
			return inboxItem{}, "", fmt.Errorf("store thumbnail: %w", err)
		}
	}
	return item, inboxWhereNone, nil
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
	var thumb []byte
	thumbTooBig := false
	up, err := receiveUploadedFile(c, dir, func(name string, part *multipart.Part) error {
		defer part.Close()
		if name == "thumb" {
			b, rerr := io.ReadAll(io.LimitReader(part, inboxThumbMaxBytes+1))
			thumb, thumbTooBig = b, len(b) > inboxThumbMaxBytes
			return rerr
		}
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
	if err != nil || c.Response().Committed { // the intake already answered
		return err
	}
	// Gone already once the bytes are stored, so this only ever cleans up a refusal.
	defer os.Remove(up.tmpPath)

	item, where, err := acceptInboxUpload(store, docs, up, fields, thumb, thumbTooBig)
	switch {
	case err != nil:
		return inboxRespond(c, err)
	case where != inboxWhereNone:
		return c.JSON(http.StatusOK, map[string]any{"status": "already_uploaded", "where": where})
	}
	// Mate sorts the item after this answer, not inside the request.
	inboxClassifierWake()
	return c.JSON(http.StatusCreated, map[string]any{
		"status": "stored",
		"item":   toInboxItemJSON(item, inboxGroupJSON{Kind: inboxGroupNoTime}, false, boatTime{placeZone: utcZone}, true), // new, so no failure to time
	})
}

// ── POST /api/inbox/known ────────────────────────────────────────────────

// inboxKnownHandler answers which of the given original-file hashes the Inbox
// already holds or has filed, so the phone can skip shrinking and sending them.
func inboxKnownHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		Hashes []string `json:"hashes"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	if len(body.Hashes) == 0 || len(body.Hashes) > inboxMaxBatch {
		return inboxError(c, http.StatusBadRequest, fmt.Sprintf("hashes must hold 1 to %d entries", inboxMaxBatch))
	}
	for _, h := range body.Hashes {
		if !validHex(h, 64, 64) {
			return inboxError(c, http.StatusBadRequest, "every hash must be 64 lowercase hex characters")
		}
	}
	known, err := store.KnownSources(body.Hashes)
	if err != nil {
		return inboxRespond(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"known": known})
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
		return inboxRespond(c, err)
	}
	passages, err := ps.ListPassages()
	if err != nil {
		return inboxRespond(c, err)
	}
	lastEnd, err := ps.LastPassageEndMs()
	if err != nil {
		return inboxRespond(c, err)
	}
	track, err := loadInboxTrack(ps, captureTimes(items))
	if err != nil {
		return inboxRespond(c, err)
	}
	// Mate's settings not being readable must not take the list down: the answer
	// is "unknown" with a notice, and the operator can still cull and file.
	var mateSorting *bool
	mateError := ""
	if on, err := inboxMateOn(); err != nil {
		log.Printf("inbox: %v", err)
		mateError = "Mate's settings couldn't be read; sorting is paused"
	} else {
		mateSorting = &on
	}
	out := make([]inboxItemJSON, 0, len(items))
	waiting := false // an item Mate has not looked at yet
	var boat boatTime
	haveBoat := false
	for _, it := range items {
		group := inboxGroupJSON{Kind: inboxGroupNoTime}
		mismatch := false
		if it.CaptureMs != nil {
			t := *it.CaptureMs
			pt := track.at(t)
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
				// The fix may be minutes old: allow for the boat's travel since.
				allowance := inboxMismatchMeters
				if pt.SOGKts != nil {
					allowance += *pt.SOGKts * inboxMismatchMeters * float64(t-pt.TMs) / float64(time.Hour.Milliseconds())
				}
				mismatch = haversineMeters(*it.PhoneLat, *it.PhoneLon, pt.Lat, pt.Lon) > allowance
			}
		}
		if it.ClassifyError != "" && !haveBoat {
			// The next try is told on the boat's clock (the last-fix rule), looked up
			// only when some item has failed.
			boat, haveBoat = boatTimeAt(0, 0, false), true
		}
		out = append(out, toInboxItemJSON(it, group, mismatch, boat, mateSorting == nil || *mateSorting)) // unknown is not "off"
		if it.SuggestedKind == "" && it.ClassifyError == "" {
			waiting = true
		}
	}
	// Items stored while Mate was off, or before a restart, are waiting with
	// nobody to sort them: reading the list with Mate on starts the classifier,
	// which does nothing if it is already running.
	if mateSorting != nil && *mateSorting && waiting {
		inboxClassifierWake()
	}
	// mate_sorting says Mate is set up, so an item with no kind yet is waiting
	// for it rather than never going to get one. It is null, with mate_error,
	// when the settings could not be read.
	body := map[string]any{"items": out, "mate_sorting": mateSorting}
	if mateError != "" {
		body["mate_error"] = mateError
	}
	return c.JSON(http.StatusOK, body)
}

// inboxSortAgainHandler is POST /api/inbox/sort-again, the manual nudge: it
// clears every failure Mate recorded, with its back-off, and wakes the
// classifier so the items are looked at again now.
func inboxSortAgainHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	n, err := store.RequeueFailed()
	if err != nil {
		return inboxRespond(c, err)
	}
	inboxClassifierWake()
	return c.JSON(http.StatusOK, map[string]int{"requeued": n})
}

// inboxCountHandler is GET /api/inbox/count, cheap enough for a badge.
func inboxCountHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	n, err := store.Count()
	if err != nil {
		return inboxRespond(c, err)
	}
	return c.JSON(http.StatusOK, map[string]int{"count": n})
}

// serveInboxFile sends one of an item's stored files.
func serveInboxFile(c echo.Context, it inboxItem, path, contentType string) error {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("inbox: open %s: %v", it.ID, err)
		return inboxError(c, http.StatusInternalServerError, "the file is missing from Inbox storage")
	}
	defer f.Close()
	h := c.Response().Header()
	h.Set(echo.HeaderContentType, contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Response(), c.Request(), it.Filename, time.UnixMilli(it.CreatedMs), f)
	return nil
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
		return inboxRespond(c, err)
	}
	return serveInboxFile(c, it, inboxFilePath(it.SHA256), it.MIME)
}

// inboxThumbHandler is GET /api/inbox/:id/thumb, the small preview of a photo.
func inboxThumbHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	it, err := store.Get(c.Param("id"))
	if errors.Is(err, errInboxItemNotFound) {
		return inboxError(c, http.StatusNotFound, err.Error())
	}
	if err != nil {
		return inboxRespond(c, err)
	}
	if !it.HasThumb {
		return inboxError(c, http.StatusNotFound, "this item has no preview")
	}
	return serveInboxFile(c, it, inboxThumbPath(it.SHA256), "image/jpeg")
}

// ── POST /api/inbox/bin ──────────────────────────────────────────────────

// removeInboxFiles deletes an item's stored file and its preview.
func removeInboxFiles(it inboxItem) error {
	for _, p := range []string{inboxFilePath(it.SHA256), inboxThumbPath(it.SHA256)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove inbox file: %w", err)
		}
	}
	return nil
}

// removeInboxItem deletes the row, then the files, under the content's lock.
func removeInboxItem(store *inboxStore, it inboxItem) error {
	unlock := lockDocumentSHA(it.SHA256)
	defer unlock()
	if err := store.Delete(it.ID); err != nil {
		return err
	}
	return removeInboxFiles(it)
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
	found, err := store.GetMany(body.IDs)
	if err != nil {
		return inboxRespond(c, err)
	}
	items := make([]inboxItem, 0, len(found))
	ids := make([]string, 0, len(found))
	for id, it := range found {
		items, ids = append(items, it), append(ids, id)
	}
	// Locked in one order, so two bins that overlap cannot wait on each other.
	sort.Slice(items, func(i, j int) bool { return items[i].SHA256 < items[j].SHA256 })
	for _, it := range items {
		defer lockDocumentSHA(it.SHA256)()
	}
	removed, err := store.DeleteUnclaimed(ids)
	if err != nil {
		log.Printf("inbox: bin: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "could not remove every item", "deleted": 0})
	}
	gone := make(map[string]bool, len(removed))
	for _, id := range removed {
		gone[id] = true
	}
	// An item left alone and still there is being filed; its files stay with it.
	// One filed (and so removed) since it was read is neither deleted nor busy,
	// and is left out of the answer.
	var left []string
	for _, it := range items {
		if !gone[it.ID] {
			left = append(left, it.ID)
		}
	}
	stillThere, err := store.GetMany(left)
	if err != nil {
		return inboxRespond(c, err)
	}
	busy := []inboxResult{}
	for _, it := range items {
		if !gone[it.ID] {
			if _, ok := stillThere[it.ID]; ok {
				busy = append(busy, inboxResult{ID: it.ID, Error: errInboxBusy.Error()})
			}
			continue
		}
		if err := removeInboxFiles(it); err != nil {
			log.Printf("inbox: bin %s: %v", it.ID, err)
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "could not remove every item", "deleted": len(removed)})
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"deleted": len(removed), "busy": busy})
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

// inboxRemoveItem clears a filed item from the Inbox. A var so a test can make it fail.
var inboxRemoveItem = removeInboxItem

// inboxRefusal is a plain-words reason an item cannot be filed as asked.
type inboxRefusal struct{ msg string }

func (e inboxRefusal) Error() string { return e.msg }

func refusedf(format string, a ...any) error { return inboxRefusal{fmt.Sprintf(format, a...)} }

// withClaimedInboxItem holds the filing claim on an item while file runs, so a
// double tap or an overlapping retry cannot make two signs from one photo. The
// item is read after the claim, so one another request just filed is gone.
func withClaimedInboxItem(store *inboxStore, id string, file func(it inboxItem) (inboxResult, error)) (inboxResult, error) {
	token, err := store.Claim(id)
	if errors.Is(err, errInboxBusy) {
		return inboxResult{}, refusedf("%s", err.Error())
	} else if err != nil {
		return inboxResult{}, err
	}
	defer func() {
		if err := store.Release(id, token); err != nil {
			log.Printf("inbox: %v", err)
		}
	}()
	it, err := store.Get(id)
	if err != nil {
		return inboxResult{}, err
	}
	return file(it)
}

// finishFiled is the tail both ways of filing share: remember the original
// before clearing, so a failure to clear still leaves the photo recognised if
// it is shared again; clear the item; wake the indexer. A failed clear comes
// back as a refusal saying clearFailed.
func finishFiled(store *inboxStore, it inboxItem, clearFailed string) error {
	if err := store.MarkFiled(it.SourceSHA256); err != nil {
		log.Printf("inbox: %v", err)
	}
	if err := inboxRemoveItem(store, it); err != nil {
		log.Printf("inbox: clearing %s: %v", it.ID, err)
		return refusedf("%s", clearFailed)
	}
	wakeDocumentIndexer()
	return nil
}

// inboxLibrary copies Inbox files into Documents. What a request needs from the
// library is worked out on first use and kept, so a batch asks once however
// many items it files, and a batch that files nothing creates nothing.
type inboxLibrary struct {
	photosFolder func() (string, error)
	enrich       func() (bool, error)
}

func newInboxLibrary(docs *documentStore) *inboxLibrary {
	return &inboxLibrary{
		photosFolder: sync.OnceValues(func() (string, error) {
			f, err := docs.FindOrCreateRootFolder(passagePhotosFolderName)
			return f.ID, err
		}),
		enrich: sync.OnceValues(func() (bool, error) {
			enrich, _, err := documentEnrichFlag("inbox: file")
			return enrich, err
		}),
	}
}

// copy makes a Document of an item's file, through the same store-and-dedupe
// step an upload takes, so the indexer picks it up. created is false when the
// library already held these bytes, in which case doc is that document.
//
// link, when not nil, attaches the document to something and runs under the
// content lock, as the photo upload endpoints' link does, so a Documents delete
// of these bytes cannot come between the copy and the link. If it fails, copy
// fails with its error and leaves nothing new behind: a document it just made
// is removed along with its file, and one that was already there is untouched.
func (l *inboxLibrary) copy(it inboxItem, folderID *string, title string, link func(documentID string) error) (doc document, created bool, err error) {
	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return document{}, false, fmt.Errorf("prepare document storage: %w", err)
	}
	enrich, err := l.enrich()
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
		func(existing document) error {
			doc, created = existing, false
			if link == nil {
				return nil
			}
			return link(existing.ID)
		},
		func(inserted document) error {
			doc, created = inserted, true
			if link == nil {
				return nil
			}
			if err := link(inserted.ID); err != nil {
				// Still under the content lock: nothing else can hold these bytes.
				removeLibraryCopy(inserted)
				return err
			}
			return nil
		},
		func(err error) error { return err },
	)
	if err != nil {
		return document{}, false, err
	}
	return doc, created, nil
}

// removeLibraryCopy deletes a document copy just made and its file. The caller
// holds the content lock, which is what makes it safe: nothing else can have
// linked to these bytes since the copy.
func removeLibraryCopy(doc document) {
	if _, err := globalDocumentStore.Delete(doc.ID); err != nil {
		log.Printf("inbox: undo library copy %s: %v", doc.ID, err)
		return
	}
	if err := os.Remove(filepath.Join(documentsDirPath(), doc.SHA256)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("inbox: undo library copy file %s: %v", doc.ID, err)
	}
}

// runInboxBatch is the shape both filing calls share. It binds the body, checks
// how many items it holds, lets start refuse the request or prepare what the
// batch shares (and return the function that files one item), then files each
// item on its own: a failure leaves that item in the Inbox and is reported
// against its id, and the others still go through.
func runInboxBatch[B, R any](c echo.Context, itemsOf func(*B) []R, idOf func(R) string,
	start func(*B) (func(R) (inboxResult, error), error)) error {
	var body B
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	items := itemsOf(&body)
	if len(items) == 0 || len(items) > inboxMaxBatch {
		return inboxError(c, http.StatusBadRequest, fmt.Sprintf("items must hold 1 to %d entries", inboxMaxBatch))
	}
	file, err := start(&body)
	if err != nil {
		return inboxRespond(c, err)
	}
	results := make([]inboxResult, 0, len(items))
	for _, req := range items {
		res, err := file(req)
		if err != nil {
			res = inboxFail(idOf(req), err)
		}
		results = append(results, res)
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}

type inboxPassageRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// TMs overrides the capture time, for a photo taken between passages that
	// the operator attaches to a passage's start or end, or one with no time.
	TMs *int64 `json:"t_ms"`
}

func fileInboxItemToPassage(store *inboxStore, ps *passageStore, lib *inboxLibrary, req inboxPassageRequest) (inboxResult, error) {
	return withClaimedInboxItem(store, req.ID, func(it inboxItem) (inboxResult, error) {
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
		if err := timeInsideAPassage(ps.db, tMs); errors.Is(err, errPassageInvalid) {
			return inboxResult{}, refusedf("that time is not inside any recorded passage")
		} else if err != nil {
			return inboxResult{}, err // a database failure, logged by inboxFail
		}
		folder, err := lib.photosFolder()
		if err != nil {
			return inboxResult{}, err
		}
		// The sign is made in the copy's link step, under the content lock, so a
		// sign that fails undoes only a document this request just made, and one
		// another request linked to meanwhile cannot be deleted from under it.
		var sign replaySign
		alreadySigned := false
		doc, _, err := lib.copy(it, &folder, "", func(documentID string) error {
			// The library already held this photo. If a sign shows it, an earlier
			// filing got as far as the sign and failed to clear the item: finish
			// that, rather than make a second sign.
			n, err := ps.SignCountForDocument(documentID)
			if err != nil {
				return err
			}
			if n > 0 {
				alreadySigned = true
				return nil
			}
			moment := true
			sign, err = inboxCreateSign(ps, replaySign{TMs: tMs, Title: title, DocumentID: &documentID, Visibility: signPassing, ShowS: signShowDef, PhotoMoment: &moment})
			return err
		})
		if err != nil {
			return inboxResult{}, err
		}
		if alreadySigned {
			if err := finishFiled(store, it, "a sign already uses this photo, but it could not be cleared from the Inbox; delete it there"); err != nil {
				return inboxResult{}, err
			}
			// Cleared, which is what the operator asked for.
			return inboxResult{ID: it.ID, OK: true, DocumentID: doc.ID}, nil
		}
		if err := finishFiled(store, it, "the sign was made, but the item could not be cleared from the Inbox; delete it there"); err != nil {
			return inboxResult{}, err
		}
		return inboxResult{ID: it.ID, OK: true, SignID: sign.ID, DocumentID: doc.ID}, nil
	})
}

// fileInboxToPassageHandler is POST /api/inbox/file-to-passage.
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
	type body struct {
		Items []inboxPassageRequest `json:"items"`
	}
	return runInboxBatch(c,
		func(b *body) []inboxPassageRequest { return b.Items },
		func(r inboxPassageRequest) string { return r.ID },
		func(*body) (func(inboxPassageRequest) (inboxResult, error), error) {
			lib := newInboxLibrary(docs)
			return func(req inboxPassageRequest) (inboxResult, error) {
				return fileInboxItemToPassage(store, ps, lib, req)
			}, nil
		})
}

type inboxDocumentRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func fileInboxItemToDocuments(store *inboxStore, lib *inboxLibrary, folderID string, req inboxDocumentRequest) (inboxResult, error) {
	return withClaimedInboxItem(store, req.ID, func(it inboxItem) (inboxResult, error) {
		doc, created, err := lib.copy(it, &folderID, strings.TrimSpace(req.Title), nil)
		if err != nil {
			return inboxResult{}, err
		}
		if !created {
			return inboxResult{}, refusedf("these bytes are already in Documents as %q; bin this copy", firstNonEmpty(doc.Title, doc.Filename))
		}
		if err := finishFiled(store, it, "the document was made, but the item could not be cleared from the Inbox; delete it there"); err != nil {
			return inboxResult{}, err
		}
		return inboxResult{ID: it.ID, OK: true, DocumentID: doc.ID}, nil
	})
}

// fileInboxToDocumentsHandler is POST /api/inbox/file-to-documents.
func fileInboxToDocumentsHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	docs, err := requireDocumentStore("inbox filing")
	if err != nil {
		return inboxError(c, http.StatusServiceUnavailable, err.Error())
	}
	type body struct {
		FolderID string                 `json:"folder_id"`
		Items    []inboxDocumentRequest `json:"items"`
	}
	return runInboxBatch(c,
		func(b *body) []inboxDocumentRequest { return b.Items },
		func(r inboxDocumentRequest) string { return r.ID },
		func(b *body) (func(inboxDocumentRequest) (inboxResult, error), error) {
			if strings.TrimSpace(b.FolderID) == "" {
				return nil, inboxHTTPError{http.StatusBadRequest, "folder_id is required"}
			}
			lib := newInboxLibrary(docs)
			return func(req inboxDocumentRequest) (inboxResult, error) {
				return fileInboxItemToDocuments(store, lib, b.FolderID, req)
			}, nil
		})
}

// ── equipment and open job photos ────────────────────────────────────────

// An equipment or job photo is an ordinary Document linked to the item, so
// these filings copy the Inbox file in with the same store-and-dedupe step an
// upload takes, then link it with the store call the photo endpoints use.

type inboxPhotoRequest struct {
	ID string `json:"id"`
}

// inboxPhotoRefusal is the refusal for an item that cannot go on where ("equipment"
// or "a job"), or nil when it can: JPEG and PNG, as the photo endpoints take.
// The Inbox never holds HEIC (inboxUploadMIME), so GIF and WebP are the photos
// this refuses.
func inboxPhotoRefusal(it inboxItem, where string) error {
	if isPhotoMIME(it.MIME) {
		return nil
	}
	switch it.MIME {
	case "application/pdf":
		return refusedf("only photos can go on %s; this is a PDF", where)
	case "image/gif":
		return refusedf("only JPEG or PNG photos can go on %s; this one is a GIF", where)
	case "image/webp":
		return refusedf("only JPEG or PNG photos can go on %s; this one is a WebP", where)
	}
	return refusedf("only JPEG or PNG photos can go on %s; this file isn't one", where)
}

// photoDestination is where Inbox photos can be filed: an equipment item or an
// open job. field is the request body's name for the target and where how a
// refusal names it. check looks the target up (nil when it can take photos);
// link attaches a copied document to it. refusal turns an error from either
// into words for the operator, and false when the error is not a plain answer.
type photoDestination struct {
	field string
	where string
	check func(id string) error
	link  func(id, documentID string) error
	// linked returns the id of the target's photo with these bytes, or "" when it has none.
	linked  func(id, sha string) (string, error)
	refusal func(err error) (string, bool)
}

// equipmentDestination is the destination for photos on an equipment item.
func equipmentDestination(docs *documentStore) photoDestination {
	return photoDestination{
		field: "equipment_id",
		where: "equipment",
		check: func(id string) error {
			ok, err := docs.EquipmentExists(id)
			if err != nil {
				return err
			}
			if !ok {
				return errEquipmentNotFound
			}
			return nil
		},
		link:    docs.AddEquipmentPhoto,
		linked:  docs.EquipmentPhotoIDBySHA,
		refusal: equipmentRefusal,
	}
}

// equipmentRefusal is the one place an equipment answer becomes words.
func equipmentRefusal(err error) (string, bool) {
	if errors.Is(err, errEquipmentNotFound) {
		return "that equipment item no longer exists", true
	}
	return "", false
}

// openJobDestination is the destination for photos on a job still open.
func openJobDestination(docs *documentStore) photoDestination {
	return photoDestination{
		field: "job_id",
		where: "a job",
		check: func(id string) error {
			status, err := docs.OpenJobStatus(id)
			if err != nil {
				return err
			}
			return requireJobOpen(status)
		},
		link:    docs.AddOpenJobPhoto,
		linked:  docs.OpenJobPhotoIDBySHA,
		refusal: jobRefusal,
	}
}

// jobRefusal is the one place a job answer becomes words; a closed job says
// what the Documents screens say.
func jobRefusal(err error) (string, bool) {
	switch {
	case errors.Is(err, errOpenJobNotFound):
		return "that job no longer exists", true
	case errors.Is(err, errOpenJobClosed):
		return errOpenJobClosed.Error(), true
	}
	return "", false
}

// clearIfAlreadyOnTarget finishes an item whose photo an earlier filing already
// linked to the target but failed to clear from the Inbox, as the passage path
// does for a sign. It is checked before the target is refused or linked again,
// so the item can leave even if the job has closed since. done is false when
// the photo is not on the target yet.
func clearIfAlreadyOnTarget(store *inboxStore, it inboxItem, targetID string, dest photoDestination) (res inboxResult, done bool, err error) {
	documentID, err := dest.linked(targetID, it.SHA256)
	if err != nil || documentID == "" {
		return inboxResult{}, false, err
	}
	if err := finishFiled(store, it, "the photo is already on "+dest.where+", but the item could not be cleared from the Inbox; delete it there"); err != nil {
		return inboxResult{}, false, err
	}
	// Cleared, which is what the operator asked for.
	return inboxResult{ID: it.ID, OK: true, DocumentID: documentID}, true, nil
}

// fileInboxItemAsPhoto files one item onto the target. The target was checked
// once for the whole request; link still refuses one deleted or closed since.
// The link runs inside the copy, under the content lock; a failure there leaves
// no new document behind (see inboxLibrary.copy).
func fileInboxItemAsPhoto(store *inboxStore, lib *inboxLibrary, id, targetID string, dest photoDestination) (inboxResult, error) {
	return withClaimedInboxItem(store, id, func(it inboxItem) (inboxResult, error) {
		if err := inboxPhotoRefusal(it, dest.where); err != nil {
			return inboxResult{}, err
		}
		if res, done, err := clearIfAlreadyOnTarget(store, it, targetID, dest); done || err != nil {
			return res, err
		}
		doc, _, err := lib.copy(it, nil, "", func(documentID string) error { return dest.link(targetID, documentID) })
		if err != nil {
			if msg, ok := dest.refusal(err); ok {
				return inboxResult{}, refusedf("%s", msg)
			}
			return inboxResult{}, err
		}
		if err := finishFiled(store, it, "the photo was added, but the item could not be cleared from the Inbox; delete it there"); err != nil {
			return inboxResult{}, err
		}
		return inboxResult{ID: it.ID, OK: true, DocumentID: doc.ID}, nil
	})
}

// Each destination binds its own body, so the other destination's key is an
// unknown field and the request is refused as an invalid body.
type inboxEquipmentBody struct {
	EquipmentID string              `json:"equipment_id"`
	Items       []inboxPhotoRequest `json:"items"`
}

type inboxJobBody struct {
	JobID string              `json:"job_id"`
	Items []inboxPhotoRequest `json:"items"`
}

// fileInboxPhotosHandler is POST /api/inbox/file-to-equipment and
// /api/inbox/file-to-open-job. targetOf reads the body's target id and items.
// The target is looked up once, before any item is claimed or copied: a missing
// or closed one refuses every item and nothing is copied.
func fileInboxPhotosHandler[B any](newDestination func(*documentStore) photoDestination, targetOf func(*B) (string, []inboxPhotoRequest)) echo.HandlerFunc {
	return func(c echo.Context) error {
		store, ok := inboxStoreOrError(c)
		if !ok {
			return nil
		}
		docs, err := requireDocumentStore("inbox filing")
		if err != nil {
			return inboxError(c, http.StatusServiceUnavailable, err.Error())
		}
		dest := newDestination(docs)
		return runInboxBatch(c,
			func(b *B) []inboxPhotoRequest { _, items := targetOf(b); return items },
			func(r inboxPhotoRequest) string { return r.ID },
			func(b *B) (func(inboxPhotoRequest) (inboxResult, error), error) {
				target, _ := targetOf(b)
				targetID := strings.TrimSpace(target)
				if targetID == "" {
					return nil, inboxHTTPError{http.StatusBadRequest, dest.field + " is required"}
				}
				if err := dest.check(targetID); err != nil {
					msg, ok := dest.refusal(err)
					if !ok {
						return nil, err
					}
					// The target is gone or closed, but an item whose photo an earlier filing
					// already put on it still clears.
					return func(req inboxPhotoRequest) (inboxResult, error) {
						return withClaimedInboxItem(store, req.ID, func(it inboxItem) (inboxResult, error) {
							// Only a photo can be on it as a photo; the same bytes as a PDF are a plain document.
							if isPhotoMIME(it.MIME) {
								if res, done, err := clearIfAlreadyOnTarget(store, it, targetID, dest); done || err != nil {
									return res, err
								}
							}
							return inboxResult{}, refusedf("%s", msg)
						})
					}, nil
				}
				lib := newInboxLibrary(docs)
				return func(req inboxPhotoRequest) (inboxResult, error) {
					return fileInboxItemAsPhoto(store, lib, req.ID, targetID, dest)
				}, nil
			})
	}
}

var (
	fileInboxToEquipmentHandler = fileInboxPhotosHandler(equipmentDestination,
		func(b *inboxEquipmentBody) (string, []inboxPhotoRequest) { return b.EquipmentID, b.Items })
	fileInboxToOpenJobHandler = fileInboxPhotosHandler(openJobDestination,
		func(b *inboxJobBody) (string, []inboxPhotoRequest) { return b.JobID, b.Items })
)

// ── POST /api/inbox/suggested-titles ─────────────────────────────────────

// inboxSuggestedTitlesHandler answers {"ids": [...]} with a suggested title for
// each item (see inboxTitleBuilder for how it is made), or "" when the item has
// no time, the track has nothing near it, or nothing is named there. A title
// that could not be made is in "errors", keyed by item, with the reason. It is a
// POST at write tier because, with Mate set up, it makes paid calls and keeps
// the titles it makes.
//
// When Mate's settings cannot be read the titles are the place or sector ones,
// and "mate_error" says so; the operator is told, nothing is swapped quietly.
func inboxSuggestedTitlesHandler(c echo.Context) error {
	store, ok := inboxStoreOrError(c)
	if !ok {
		return nil
	}
	ps, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := bindPassageBody(c, &body); err != nil || c.Response().Committed {
		return err
	}
	titles := map[string]string{}
	var ids []string
	for _, id := range body.IDs {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if _, dup := titles[id]; dup {
			continue
		}
		if len(titles) >= inboxMaxBatch {
			return inboxError(c, http.StatusBadRequest, fmt.Sprintf("at most %d ids", inboxMaxBatch))
		}
		titles[id] = ""
		ids = append(ids, id)
	}
	items, err := store.GetMany(ids)
	if err != nil {
		return inboxRespond(c, err)
	}
	found := make([]inboxItem, 0, len(items))
	for _, id := range ids { // in the order asked, so the lookups are too
		if it, ok := items[id]; ok {
			found = append(found, it)
		}
	}
	track, err := loadInboxTrack(ps, captureTimes(found))
	if err != nil {
		return inboxRespond(c, err)
	}
	// Settings that cannot be read must not fail every title: the rows fall back
	// to the no-Mate path and the operator is told once.
	mateError := ""
	var mate *inboxMateSession
	if sess, mateOn, err := globalInboxMate.session(); err != nil {
		log.Printf("inbox: %v", err)
		mateError = "Mate's settings couldn't be read; these are place-name titles"
	} else if mateOn {
		mate = &sess
	}

	builder := newInboxTitleBuilder(mate, inboxFeatures, store)
	for _, it := range found {
		if it.CaptureMs == nil {
			continue
		}
		if pt := track.at(*it.CaptureMs); pt != nil {
			builder.add(it, pt)
		}
	}

	made, failures := runInboxTitleTasks(c.Request().Context(), builder.tasks)
	maps.Copy(titles, made)
	out := map[string]any{"titles": titles, "errors": failures}
	if mateError != "" {
		out["mate_error"] = mateError
	}
	return c.JSON(http.StatusOK, out)
}

// passagePhotosFolderHandler is POST /api/passage-photos-folder: the "Passage
// photos" folder every photo sign is kept in, found or made. The Inbox and the
// Replay sign editor both go through it, so the folder has one rule (ADR 0282).
func passagePhotosFolderHandler(c echo.Context) error {
	docs, err := requireDocumentStore("passage photos folder")
	if err != nil {
		return inboxError(c, http.StatusServiceUnavailable, err.Error())
	}
	folder, err := docs.FindOrCreateRootFolder(passagePhotosFolderName)
	if err != nil {
		return inboxRespond(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"id": folder.ID, "name": folder.Name})
}

// inboxRoutes returns the /api routes for the Inbox, each with its auth tier.
func inboxRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/inbox", tierRead, listInboxHandler},
		{http.MethodGet, "/api/inbox/count", tierRead, inboxCountHandler},
		{http.MethodGet, "/api/inbox/:id/content", tierRead, inboxContentHandler},
		{http.MethodGet, "/api/inbox/:id/thumb", tierRead, inboxThumbHandler},
		// A POST that only reads: the hashes are too many for a query string.
		{http.MethodPost, "/api/inbox/known", tierRead, inboxKnownHandler},
		// Write tier: with Mate set up it makes paid calls and keeps the titles.
		{http.MethodPost, "/api/inbox/suggested-titles", tierWrite, inboxSuggestedTitlesHandler},
		{http.MethodPost, "/api/inbox", tierWrite, uploadInboxHandler},
		{http.MethodPost, "/api/inbox/bin", tierWrite, binInboxHandler},
		{http.MethodPost, "/api/inbox/sort-again", tierWrite, inboxSortAgainHandler},
		{http.MethodPost, "/api/inbox/file-to-passage", tierWrite, fileInboxToPassageHandler},
		{http.MethodPost, "/api/inbox/file-to-documents", tierWrite, fileInboxToDocumentsHandler},
		{http.MethodPost, "/api/inbox/file-to-equipment", tierWrite, fileInboxToEquipmentHandler},
		{http.MethodPost, "/api/inbox/file-to-open-job", tierWrite, fileInboxToOpenJobHandler},
		{http.MethodPost, "/api/passage-photos-folder", tierWrite, passagePhotosFolderHandler},
	}
}
