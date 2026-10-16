package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureStores remembers the store each test's fixture opened, so a second
// call in the same test reuses it.
var fixtureStores = map[*testing.T]*documentStore{}

// vesselFixtureStore opens a document store for t, installs it as the global
// one and puts the previous one back when the test ends.
func vesselFixtureStore(t *testing.T) *documentStore {
	t.Helper()
	if store, ok := fixtureStores[t]; ok {
		return store
	}
	// A test that already opened a store (and put it in place) keeps it, so the
	// items it made are the ones the fixture maps.
	if globalDocumentStore != nil {
		fixtureStores[t] = globalDocumentStore
		t.Cleanup(func() { delete(fixtureStores, t); invalidateSourceItems() })
		return globalDocumentStore
	}
	store, err := newDocumentStore(filepath.Join(t.TempDir(), "documents.sqlite"))
	if err != nil {
		t.Fatalf("open fixture store: %v", err)
	}
	previous := globalDocumentStore
	globalDocumentStore = store
	invalidateSourceItems()
	fixtureStores[t] = store
	t.Cleanup(func() {
		delete(fixtureStores, t)
		globalDocumentStore = previous
		invalidateSourceItems()
		_ = store.Close()
	})
	return store
}

// saveVesselFixture sets the vessel up the way an operator does now: equipment
// items that own their SignalK sources, and a roles block naming the ones that
// play a part. v is the resolved shape the readers see, so a test states what
// it wants read and gets exactly that back from loadVesselView. Items named in
// v by an existing EquipmentID are reused; any other gets a new item.
func saveVesselFixture(t *testing.T, settingsPath string, v vesselView) error {
	t.Helper()
	store := vesselFixtureStore(t)
	byID := map[string]string{} // fixture id -> real id
	bySource := map[string]string{}
	// A second call in the same test updates the items the first made.
	existing, err := store.SourceItems()
	if err != nil {
		return err
	}
	// The view is authoritative: an item the earlier call mapped and this one
	// does not mention loses its source (the operator "unticked" it), though it
	// stays available to be mapped again.
	for _, it := range existing {
		bySource[it.Source] = it.ID
		item, err := store.GetEquipment(it.ID)
		if err != nil {
			return err
		}
		item.SignalKSource, item.CapacityAh, item.Cells, item.WarnVoltage, item.HighVoltage = "", 0, 0, 0, 0
		if _, err := store.UpdateEquipment(it.ID, item); err != nil {
			return err
		}
	}

	ensure := func(source, name, wantID string, edit func(*equipmentItem)) (string, error) {
		if id, ok := bySource[source]; ok {
			item, err := store.GetEquipment(id)
			if err != nil {
				return "", err
			}
			item.SignalKSource = source
			if name != "" {
				item.Name = name
			}
			if edit != nil {
				edit(&item)
			}
			if _, err := store.UpdateEquipment(id, item); err != nil {
				return "", err
			}
			return id, nil
		}
		if wantID != "" {
			if item, err := store.GetEquipment(wantID); err == nil {
				item.SignalKSource = source
				if edit != nil {
					edit(&item)
				}
				if _, err := store.UpdateEquipment(wantID, item); err != nil {
					return "", err
				}
				bySource[source], byID[wantID] = wantID, wantID
				return wantID, nil
			}
		}
		if name == "" {
			name = source
		}
		item := equipmentItem{Name: name, System: sourceSystem(sourceGroupOf(source)), Quantity: 1, SignalKSource: source}
		if edit != nil {
			edit(&item)
		}
		created, err := store.CreateEquipment(item)
		if err != nil {
			return "", fmt.Errorf("fixture item %s: %w", source, err)
		}
		bySource[source] = created.ID
		return created.ID, nil
	}

	var roles vesselRoles
	for _, e := range v.Engines {
		if _, err := ensure(sourcePath(sourceEngine, e.Instance), e.Name, e.EquipmentID, nil); err != nil {
			return err
		}
	}
	for _, b := range v.Batteries {
		if _, err := ensure(sourcePath(sourceBattery, b.Instance), b.Name, "", nil); err != nil {
			return err
		}
	}
	for _, s := range v.Solar {
		if _, err := ensure(sourcePath(sourceSolar, s.Instance), s.Name, "", nil); err != nil {
			return err
		}
	}
	for key, name := range v.Tanks {
		if _, err := ensure("tanks."+key, name, "", nil); err != nil {
			return err
		}
	}
	if hb := v.HouseBank; hb != nil {
		id, err := ensure(hb.Path, "House bank", hb.EquipmentID, func(it *equipmentItem) {
			it.CapacityAh, it.Cells, it.WarnVoltage, it.HighVoltage = hb.CapacityAh, hb.Cells, hb.WarnVoltage, hb.HighVoltage
		})
		if err != nil {
			return err
		}
		roles.HouseBank = id
	}
	role := func(group sourceGroup, instance string) (string, error) {
		if instance == "" {
			return "", nil
		}
		return ensure(sourcePath(group, instance), "", "", nil)
	}
	if roles.PortAlternator, err = role(sourceAlternator, v.Alternators.PortInstance); err != nil {
		return err
	}
	if roles.StarboardAlternator, err = role(sourceAlternator, v.Alternators.StarboardInstance); err != nil {
		return err
	}
	if roles.Generator, err = role(sourceGenerator, v.Electrical.GeneratorInstance); err != nil {
		return err
	}
	if roles.ShoreCharger, err = role(sourceCharger, v.Electrical.ShoreChargerInstance); err != nil {
		return err
	}
	if roles.Inverter, err = role(sourceInverter, v.Electrical.InverterInstance); err != nil {
		return err
	}
	if roles.VoltageReadoutBattery, err = role(sourceBattery, v.Electrical.VoltageReadoutBattery); err != nil {
		return err
	}
	roles.GeneratorACInstance = v.Electrical.GeneratorACInstance
	for _, spec := range roleSpecs {
		if v.RolesNone[spec.field] {
			*spec.get(&roles) = roleNone
		}
	}
	if v.RolesNone["generator_ac_instance"] {
		roles.GeneratorACInstance = roleNone
	}

	invalidateSourceItems()
	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}
	block, err := rolesBlockMap(roles)
	if err != nil {
		return err
	}
	settings["roles"] = block
	return writeSettings(settingsPath, settings)
}

// sourceGroupOf is the group of a source the fixture built, for choosing a
// new item's system.
func sourceGroupOf(source string) sourceGroup {
	group, _, _ := parseSource(strings.TrimSpace(source))
	return group
}
