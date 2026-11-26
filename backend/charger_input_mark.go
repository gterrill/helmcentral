package main

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/labstack/echo/v4"
)

/*
The operator's mark on a battery instance: "a charger's input, not a battery".

sensorNamer.chargerInput recognises a solar charger's panel voltage on its own,
but only when the charger also feeds a bank another device publishes. A lone
MPPT that publishes just one instance fails that test and its 80 V panel
reading is range-checked as a battery. The operator can say so instead. The
mark is kept in helmcentral.sqlite (charger_input_mark_store.go) and is empty
until the operator sets one.

The mark is stored as given and judged each time it is read: it applies only
while no item owns the number, something publishes it and no publisher is a
known non-charger; a device not yet known leaves it in effect (markApplies and
its siblings in sensor_names.go). Nothing that changes
an item or a publisher later has to remember to clear it.
*/

// chargerInputInstanceRe is the <id> a battery instance may have, the same
// characters the battery source path accepts (vessel_sources.go).
var chargerInputInstanceRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// markChargerInput sets or clears the mark on battery instance id. Both are
// idempotent. The handler checks the id; a nil store is refused by the store.
func markChargerInput(id string, marked bool) error {
	return globalChargerInputMarks.set(id, marked, time.Now().UTC())
}

// PUT /api/vessel/charger-inputs/:instance with {"marked": true|false}.
func putChargerInputHandler(c echo.Context) error {
	id := c.Param("instance")
	if !chargerInputInstanceRe.MatchString(id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "the battery instance is not a valid id"})
	}
	var req struct {
		Marked *bool `json:"marked"`
	}
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.Marked == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": `send {"marked": true} or {"marked": false}`})
	}
	if *req.Marked {
		if refused := refuseChargerInputMark(id); refused != nil {
			return c.JSON(refused.status, map[string]string{"error": refused.message})
		}
	}
	if err := markChargerInput(id, *req.Marked); err != nil {
		log.Printf("charger input mark: could not change battery %s: %v", id, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Couldn't save the charger-input mark. Try again; if it keeps failing, see Settings → Logs."})
	}
	return c.JSON(http.StatusOK, map[string]any{"instance": id, "marked": *req.Marked})
}

type chargerInputRefusal struct {
	status  int
	message string
}

// refuseChargerInputMark says why battery instance id can't be marked now, or
// nil when it can. It asks the one rule (sensorNamer.chargerInput) and refuses
// unless a mark would apply. Clearing a mark is never refused.
func refuseChargerInputMark(id string) *chargerInputRefusal {
	if globalSignalKSnapshot == nil {
		return &chargerInputRefusal{http.StatusServiceUnavailable, "Helmcentral has no data from SignalK yet, so it can't tell what reports battery " + id + "."}
	}
	items, err := listSourceItems()
	if err != nil {
		return &chargerInputRefusal{http.StatusInternalServerError, "Couldn't read the equipment items."}
	}
	owner, owned := "", false
	for _, it := range items {
		if it.Source == sourcePath(sourceBattery, id) {
			owner, owned = it.Name, true
			break
		}
	}
	namer := newSensorNamer(globalSignalKSnapshot, vesselView{}, globalSourceDevices.get())
	// One read of the rule: a mark applies exactly when the discovery row's
	// CanMark is true, and any other state says why it can't.
	mark := namer.chargerInput(id, true, owned).Mark
	if mark == markApplies {
		return nil
	}
	return chargerInputRefusalFor(id, mark, owner)
}

// chargerInputRefusalFor words why a mark in state mark can't apply. A state it
// doesn't know still refuses.
func chargerInputRefusalFor(id, mark, owner string) *chargerInputRefusal {
	switch mark {
	case markAutomatic:
		return &chargerInputRefusal{http.StatusConflict, "Helmcentral already treats battery instance " + id + " as a charger's input."}
	case markItem:
		return &chargerInputRefusal{http.StatusConflict, "Battery " + id + " belongs to " + owner + ", so it is a battery. Move the item to another source first."}
	case markNoPublishers:
		return &chargerInputRefusal{http.StatusConflict, "Nothing has reported battery " + id + " since Helmcentral started, so there is no charger to mark it for."}
	case markNotChargers:
		return &chargerInputRefusal{http.StatusConflict, "A device that isn't a charger also reports battery " + id + ", so it could be a real battery."}
	}
	return &chargerInputRefusal{http.StatusConflict, "Helmcentral can't mark battery instance " + id + " as a charger's input right now."}
}
