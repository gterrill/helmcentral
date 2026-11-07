package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// The Open jobs REST surface (ADR 0269), under /api/inventory/open-jobs. Each
// write runs the same command Mate's changeset runs (open_jobs.go). The
// operator's own date comes in as ?today= wherever a job is opened or dropped,
// the way every maintenance write takes it; nothing here reads the server's
// clock for a date.

type openJobDropRequest struct {
	Reason string `json:"reason"`
}

func listOpenJobsHandler(c echo.Context) error {
	status := strings.TrimSpace(c.QueryParam("status"))
	switch status {
	case "", openJobOpen, openJobDone, openJobDropped, openJobStatusAny:
	default:
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "status", Message: "status must be open, done, dropped or any"})
	}
	equipmentID := strings.TrimSpace(c.QueryParam("equipment"))
	system := strings.TrimSpace(c.QueryParam("system"))
	if system != "" && !validEquipmentSystems[system] {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "system", Message: "unknown system"})
	}
	jobs, err := globalDocumentStore.ListOpenJobs(openJobFilter{
		EquipmentID: equipmentID,
		System:      system,
		Status:      status,
		// An item's own page lists the item's jobs even when it is in storage.
		IncludeStored: equipmentID != "" || c.QueryParam("include_stored") == "true",
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"jobs": jobs})
}

func getOpenJobHandler(c echo.Context) error {
	job, err := globalDocumentStore.GetOpenJob(c.Param("id"))
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"job": job})
}

func createOpenJobHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req openJobRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	job, err := globalDocumentStore.CreateOpenJobCmd(today, req)
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"job": job})
}

func updateOpenJobHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req openJobRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	job, err := globalDocumentStore.UpdateOpenJobCmd(c.Param("id"), req)
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"job": job})
}

// completeOpenJobHandler is POST /api/inventory/open-jobs/:id/done: the
// service log form, answered with the closed job and the line it wrote.
func completeOpenJobHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req maintenanceLogEntryRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	job, entry, err := globalDocumentStore.CompleteOpenJobCmd(c.Param("id"), req)
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"job": job, "entry": entry})
}

// dropOpenJobHandler is POST /api/inventory/open-jobs/:id/drop, with an
// optional {"reason"}.
func dropOpenJobHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req openJobDropRequest
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
	}
	job, err := globalDocumentStore.DropOpenJobCmd(today, c.Param("id"), req.Reason)
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"job": job})
}

func respondWithOpenJob(c echo.Context, id string, status int) error {
	job, err := globalDocumentStore.GetOpenJob(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(status, map[string]any{"job": job})
}

// uploadOpenJobPhotoHandler is POST /api/inventory/open-jobs/:id/photos
// (multipart, one "file" part), the same intake a service log photo uses.
func uploadOpenJobPhotoHandler(c echo.Context) error {
	id := c.Param("id")
	job, err := globalDocumentStore.GetOpenJob(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if job.Status != openJobOpen {
		return writeDocumentError(c, errOpenJobClosed)
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to prepare document storage"})
	}
	up, err := receiveUploadedFile(c, dir, nil)
	if err != nil {
		return err
	}
	mimeType := detectDocumentMIME(up.head, up.filename)
	switch mimeType {
	case "image/jpeg", "image/png":
	case "image/heic":
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": documentHEICRejectionMessage})
	default:
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "only JPEG or PNG photos are accepted"})
	}
	enrich, _, err := documentEnrichFlag("open jobs: upload photo")
	if err != nil {
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	doc := document{Filename: up.filename, MIME: mimeType, SizeBytes: up.size, Enrich: enrich}

	return storeUploadedFile(dir, up, doc,
		func(existing document) error {
			if err := globalDocumentStore.AddOpenJobPhoto(id, existing.ID); err != nil {
				return writeDocumentError(c, err)
			}
			return respondWithOpenJob(c, id, http.StatusOK)
		},
		func(inserted document) error {
			if err := globalDocumentStore.AddOpenJobPhoto(id, inserted.ID); err != nil {
				return writeDocumentError(c, err)
			}
			wakeDocumentIndexer()
			return respondWithOpenJob(c, id, http.StatusCreated)
		},
		func(err error) error {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		},
	)
}

// deleteOpenJobPhotoHandler is DELETE
// /api/inventory/open-jobs/:id/photos/:documentId[?delete=true]: unlink by
// default; delete=true also deletes the document once nothing else shows it.
func deleteOpenJobPhotoHandler(c echo.Context) error {
	id := c.Param("id")
	documentID := c.Param("documentId")
	deleteDoc := c.QueryParam("delete") == "true"

	doc, err := globalDocumentStore.Get(documentID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	unlockSHA := lockDocumentSHA(doc.SHA256)
	defer unlockSHA()

	if err := globalDocumentStore.RemoveOpenJobPhoto(id, documentID); err != nil {
		return writeDocumentError(c, err)
	}
	if deleteDoc {
		deletable, err := globalDocumentStore.OpenJobPhotoDeletableAsOrphan(documentID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("the photo was removed from the job, but checking whether it was safe to delete failed: %v", err),
			})
		}
		if deletable {
			sha, err := globalDocumentStore.Delete(documentID)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("the photo was removed from the job, but deleting it failed: %v", err),
				})
			}
			if err := removeDocumentFile(sha); err != nil {
				log.Printf("open jobs: delete photo: failed to remove file %s: %v", filepath.Join(documentsDirPath(), sha), err)
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "the photo was removed from the job, but its file could not be removed from disk",
				})
			}
		}
	}
	return respondWithOpenJob(c, id, http.StatusOK)
}

// openJobRoutes returns the /api routes for open jobs. Each row carries its
// own auth tier (docs/adr/0177).
func openJobRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/inventory/open-jobs", tierRead, listOpenJobsHandler},
		{http.MethodGet, "/api/inventory/open-jobs/:id", tierRead, getOpenJobHandler},
		{http.MethodPost, "/api/inventory/open-jobs", tierWrite, createOpenJobHandler},
		{http.MethodPut, "/api/inventory/open-jobs/:id", tierWrite, updateOpenJobHandler},
		{http.MethodPost, "/api/inventory/open-jobs/:id/done", tierWrite, completeOpenJobHandler},
		{http.MethodPost, "/api/inventory/open-jobs/:id/drop", tierWrite, dropOpenJobHandler},
		{http.MethodPost, "/api/inventory/open-jobs/:id/photos", tierWrite, uploadOpenJobPhotoHandler},
		{http.MethodDelete, "/api/inventory/open-jobs/:id/photos/:documentId", tierWrite, deleteOpenJobPhotoHandler},
	}
}
