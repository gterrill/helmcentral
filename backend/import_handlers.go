package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// This file serves the import API (import_store.go keeps the runs,
// import_commit.go writes them): upload an export, read and adjust the
// decisions page by page, hand over the files the export only links to,
// commit, abandon. A run lives on the server so the wizard resumes on another
// device; nothing in it touches the registry until commit.

// importMaxUploadBytes caps an export file. A real Vessel Export is around
// 100 KB; ten megabytes is generous and still small enough to read whole.
const importMaxUploadBytes = 10 << 20

// importParsers maps a source name to the parser that turns its export into
// the platform-neutral payload. A later source is another entry here.
var importParsers = map[string]func([]byte) (stagedImport, error){
	importSourceYachtWave: parseYachtWaveExport,
}

func importError(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]string{"error": msg})
}

// createImportRunHandler is POST /api/import/runs: multipart with the export
// as "file" and the source name as "source". A file that is not an export of
// that source is a 400 saying so; nothing is stored for it.
func createImportRunHandler(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, importMaxUploadBytes)

	// Parse the body here so a too-large upload is reported as that, rather
	// than surfacing later as an empty "source" field.
	if err := c.Request().ParseMultipartForm(importMaxUploadBytes); err != nil {
		var tooBig *http.MaxBytesError
		if asMaxBytes(err, &tooBig) {
			return importError(c, http.StatusRequestEntityTooLarge, "the export file is too large")
		}
		return importError(c, http.StatusBadRequest, "send the export as multipart form data with \"source\" and \"file\" fields")
	}

	source := strings.TrimSpace(c.FormValue("source"))
	parse, known := importParsers[source]
	if !known {
		names := make([]string, 0, len(importParsers))
		for n := range importParsers {
			names = append(names, n)
		}
		return importError(c, http.StatusBadRequest, fmt.Sprintf("unknown import source %q (supported: %s); send it as the \"source\" field", source, strings.Join(names, ", ")))
	}

	fh, err := c.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if asMaxBytes(err, &tooBig) {
			return importError(c, http.StatusRequestEntityTooLarge, "the export file is too large")
		}
		return importError(c, http.StatusBadRequest, "send the export as a multipart \"file\" field")
	}
	f, err := fh.Open()
	if err != nil {
		return importError(c, http.StatusBadRequest, "could not read the uploaded file")
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return importError(c, http.StatusBadRequest, "could not read the uploaded file")
	}
	if len(raw) == 0 {
		return importError(c, http.StatusBadRequest, "the uploaded file is empty")
	}

	staged, err := parse(raw)
	if err != nil {
		return importError(c, http.StatusBadRequest, err.Error())
	}
	decisions, err := globalDocumentStore.DefaultImportDecisions(staged)
	if err != nil {
		return writeDocumentError(c, err)
	}

	label := fmt.Sprintf("%s export of %s (%s", importSourceTitle(source), staged.Vessel.Name, filepath.Base(fh.Filename))
	if staged.Vessel.GeneratedAt != "" {
		label += ", generated " + staged.Vessel.GeneratedAt
	}
	label += ")"

	run, err := globalDocumentStore.CreateImportRun(source, label, sha256Hex(raw), staged, decisions)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, run)
}

func importSourceTitle(source string) string {
	if source == importSourceYachtWave {
		return "YachtWave"
	}
	return source
}

// asMaxBytes reports whether err is the body-too-large error.
func asMaxBytes(err error, target **http.MaxBytesError) bool {
	for err != nil {
		if e, ok := err.(*http.MaxBytesError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// getImportRunHandler is GET /api/import/runs/:id.
func getImportRunHandler(c echo.Context) error {
	run, err := globalDocumentStore.GetImportRun(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, run)
}

// patchImportRunHandler is PATCH /api/import/runs/:id with {decisions}. The
// decisions are merged into the stored ones entry by entry (see
// MergeImportDecisions); a whole set that does not validate is a 400 and
// saves nothing.
func patchImportRunHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req struct {
		Decisions *importDecisions `json:"decisions"`
	}
	if err := c.Bind(&req); err != nil {
		return importError(c, http.StatusBadRequest, "invalid body")
	}
	if req.Decisions == nil {
		return importError(c, http.StatusBadRequest, "send {\"decisions\": {...}}")
	}
	run, err := globalDocumentStore.MergeImportDecisions(c.Param("id"), *req.Decisions)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, run)
}

// uploadImportFileHandler is POST /api/import/runs/:id/files/:key: the bytes
// of one photo or document the export only linked to. It goes through the
// same intake as POST /api/documents (receiveUploadedFile/storeUploadedFile:
// hashed, deduplicated, indexed), lands unfiled, and its id is saved in the
// run's decisions; commit does any linking. A photo slot takes JPEG or PNG
// only, as the equipment photo route does.
func uploadImportFileHandler(c echo.Context) error {
	id, key := c.Param("id"), c.Param("key")

	run, err := globalDocumentStore.GetImportRun(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if run.Status != importStatusDraft {
		return writeDocumentError(c, errImportRunNotDraft)
	}
	var file *stagedFile
	for i := range run.Staged.Files {
		if run.Staged.Files[i].Key == key {
			file = &run.Staged.Files[i]
		}
	}
	if file == nil {
		return writeDocumentError(c, importInvalidf("no file %q in this export", key))
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return importError(c, http.StatusInternalServerError, "failed to prepare document storage")
	}
	up, err := receiveUploadedFile(c, dir, nil)
	if err != nil {
		return err
	}

	mimeType := detectDocumentMIME(up.head, up.filename)
	if file.Kind == stagedFileKindPhoto {
		switch mimeType {
		case "image/jpeg", "image/png":
		case "image/heic":
			os.Remove(up.tmpPath)
			return importError(c, http.StatusBadRequest, documentHEICRejectionMessage)
		default:
			os.Remove(up.tmpPath)
			return importError(c, http.StatusBadRequest, "only JPEG or PNG photos are accepted")
		}
	}

	enrich, _, err := documentEnrichFlag("import: upload file")
	if err != nil {
		os.Remove(up.tmpPath)
		return importError(c, http.StatusInternalServerError, err.Error())
	}

	doc := document{Filename: up.filename, Title: file.Label, MIME: mimeType, SizeBytes: up.size, Enrich: enrich}

	respond := func(stored document, status int, duplicate bool) error {
		updated, err := globalDocumentStore.SetImportFileDocument(id, key, stored.ID)
		if err != nil {
			return writeDocumentError(c, err)
		}
		wakeDocumentIndexer()
		return c.JSON(status, map[string]any{"document_id": stored.ID, "duplicate": duplicate, "run": updated})
	}
	return storeUploadedFile(dir, up, doc,
		func(existing document) error { return respond(existing, http.StatusOK, true) },
		func(inserted document) error { return respond(inserted, http.StatusCreated, false) },
		func(err error) error { return importError(c, http.StatusInternalServerError, err.Error()) },
	)
}

// commitImportRunHandler is POST /api/import/runs/:id/commit.
func commitImportRunHandler(c echo.Context) error {
	result, err := commitImportRun(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, result)
}

// deleteImportRunHandler is DELETE /api/import/runs/:id: abandons a draft. It
// answers with the run (status abandoned) rather than 204, so the wizard
// can show what it just closed.
func deleteImportRunHandler(c echo.Context) error {
	run, err := globalDocumentStore.AbandonImportRun(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, run)
}
