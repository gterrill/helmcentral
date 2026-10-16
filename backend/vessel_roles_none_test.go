package main

import (
	"strings"
	"testing"
)

func noneItems() []sourceItem {
	return []sourceItem{{ID: "b1", Name: "House bank", Source: "electrical.batteries.512", Group: sourceBattery, Instance: "512"}}
}

func TestRolesAcceptNoneForOptionalEquipment(t *testing.T) {
	r := vesselRoles{HouseBank: "b1", PortAlternator: roleNone, StarboardAlternator: roleNone, ShoreCharger: roleNone,
		Inverter: roleNone, Generator: roleNone, GeneratorACInstance: roleNone}
	if err := validateVesselRoles(r, noneItems(), nil); err != nil {
		t.Fatalf("none is valid for optional roles: %+v", err)
	}
}

func TestRolesRefuseNoneForRequiredParts(t *testing.T) {
	for _, r := range []vesselRoles{{HouseBank: roleNone}, {VoltageReadoutBattery: roleNone}} {
		err := validateVesselRoles(r, noneItems(), nil)
		if err == nil || !strings.Contains(err.Message, "can't be") {
			t.Fatalf("none must be refused for %+v, got %+v", r, err)
		}
	}
}

func TestRolesNoneOnBothAlternatorsIsNotACollision(t *testing.T) {
	r := vesselRoles{PortAlternator: roleNone, StarboardAlternator: roleNone}
	if err := validateVesselRoles(r, nil, nil); err != nil {
		t.Fatalf("both none is fine: %+v", err)
	}
}

// none reads exactly like unset for data: no figures, no log noise, no error.
func TestResolveTreatsNoneAsUnsetButRemembersTheAnswer(t *testing.T) {
	resetVesselReadLog()
	roles := vesselRoles{PortAlternator: roleNone, ShoreCharger: roleNone, Inverter: roleNone, Generator: roleNone, GeneratorACInstance: roleNone}
	v := resolveVesselView(roles, nil)
	if v.Alternators.PortInstance != "" || v.Electrical.ShoreChargerInstance != "" || v.Electrical.InverterInstance != "" ||
		v.Electrical.GeneratorInstance != "" || v.Electrical.GeneratorACInstance != "" {
		t.Fatalf("none must resolve as unset: %+v", v)
	}
	for _, field := range []string{"port_alternator", "shore_charger", "inverter", "generator", "generator_ac_instance"} {
		if !v.RolesNone[field] {
			t.Errorf("%s: the none answer is remembered", field)
		}
	}
	if v.RolesNone["starboard_alternator"] {
		t.Error("an unset role is not none")
	}
	vesselReadLog.mu.Lock()
	defer vesselReadLog.mu.Unlock()
	if len(vesselReadLog.last) != 0 {
		t.Fatalf("none must not log a problem: %+v", vesselReadLog.last)
	}
}

func TestDeletingAnItemKeepsNoneAnswers(t *testing.T) {
	path := writeVesselTestSettings(t, "roles:\n  house_bank: b1\n  inverter: none\n")
	cleared, err := clearRolesForItem(path, "b1")
	if err != nil || len(cleared) != 1 {
		t.Fatalf("cleared=%v err=%v", cleared, err)
	}
	roles, err := loadVesselRoles(path)
	if err != nil || roles.Inverter != roleNone || roles.HouseBank != "" {
		t.Fatalf("roles after delete: %+v err=%v", roles, err)
	}
}
