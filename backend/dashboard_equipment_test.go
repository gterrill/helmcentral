package main

import "testing"

// newTileItem creates an equipment item that owns source ("" for none) and
// follows profileID, and returns its id. The store is the test's own.
func newTileItem(t *testing.T, name, source, profileID string) string {
	t.Helper()
	store := vesselFixtureStore(t)
	item := equipmentItem{Name: name, System: "propulsion", Quantity: 1, SignalKSource: source, ProfileID: profileID}
	if source != "" {
		item.System = sourceSystem(sourceGroupOf(source))
	}
	created, err := store.CreateEquipment(item)
	if err != nil {
		t.Fatalf("create item %q: %v", name, err)
	}
	invalidateSourceItems()
	return created.ID
}

// useProfiles installs profiles as the loaded set for the test.
func useProfiles(t *testing.T, profiles ...engineProfile) {
	t.Helper()
	previous, problems := engineProfiles()
	storeEngineProfiles(profiles, nil)
	t.Cleanup(func() { storeEngineProfiles(previous, problems) })
}

// An edit that lands while a read is in flight must not be undone by that
// read: the stale refs it fetched may not be cached as fresh.
func TestListEquipmentRefsDoesNotCacheARefreshThatRacedAnInvalidate(t *testing.T) {
	store := vesselFixtureStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Port engine", System: "propulsion", Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	invalidateSourceItems()

	equipmentRefsReadDone = func() {
		equipmentRefsReadDone = nil
		// The operator renames the item after the read returned, before the
		// refresh is stored.
		if _, err := store.db.Exec(`UPDATE equipment SET name = 'Renamed' WHERE id = ?`, item.ID); err != nil {
			t.Fatal(err)
		}
		invalidateEquipmentRefs()
	}
	t.Cleanup(func() { equipmentRefsReadDone = nil })

	if _, err := listEquipmentRefs(); err != nil {
		t.Fatal(err)
	}
	refs, err := listEquipmentRefs()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if r.ID == item.ID && r.Name != "Renamed" {
			t.Fatalf("stale refs served after an invalidate: %+v", r)
		}
	}
}
