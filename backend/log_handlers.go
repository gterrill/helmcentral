package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

// getLogsHandler returns the log entries recorded in memory.
// Query param: since=<int64 id> (optional)
func getLogsHandler(c echo.Context) error {
	sinceStr := c.QueryParam("since")
	if sinceStr != "" {
		sinceID, err := strconv.ParseInt(sinceStr, 10, 64)
		if err == nil {
			return c.JSON(http.StatusOK, globalLogBuffer.entriesSince(sinceID))
		}
	}
	return c.JSON(http.StatusOK, globalLogBuffer.entries())
}

// logsStreamHandler streams log entries live to the client via Server-Sent Events.
func logsStreamHandler(c echo.Context) error {
	response := c.Response()
	header := response.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)

	ctx := c.Request().Context()

	// Replay recent backlog up to 200 entries or entries since param
	var backlog []logEntry
	sinceStr := c.QueryParam("since")
	if sinceStr != "" {
		if sinceID, err := strconv.ParseInt(sinceStr, 10, 64); err == nil {
			backlog = globalLogBuffer.entriesSince(sinceID)
		} else {
			backlog = globalLogBuffer.entries()
		}
	} else {
		backlog = globalLogBuffer.entries()
	}

	// If backlog is larger than 200, take the last 200
	if len(backlog) > 200 {
		backlog = backlog[len(backlog)-200:]
	}

	for _, entry := range backlog {
		encoded, err := json.Marshal(entry)
		if err == nil {
			if _, err := fmt.Fprintf(response, "event: log\ndata: %s\n\n", string(encoded)); err != nil {
				return nil
			}
		}
	}
	response.Flush()

	ch, unsubscribe := globalLogBuffer.subscribe()
	defer unsubscribe()

	keepaliveTicker := time.NewTicker(15 * time.Second)
	defer keepaliveTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-keepaliveTicker.C:
			if _, err := fmt.Fprintf(response, ": keepalive\n\n"); err != nil {
				return nil
			}
			response.Flush()
		case entry, ok := <-ch:
			if !ok {
				return nil
			}
			encoded, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(response, "event: log\ndata: %s\n\n", string(encoded)); err != nil {
				return nil
			}
			response.Flush()
		}
	}
}
