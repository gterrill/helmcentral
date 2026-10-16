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
