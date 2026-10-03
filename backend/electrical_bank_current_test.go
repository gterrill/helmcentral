package main

import "testing"

func TestFetchSignalKElectricalState_BankCurrentUnknownIsNil(t *testing.T) {
	seedSelfTree(t, `{
		"timestamp": "2026-09-02T00:00:00Z",
		"electrical": {"batteries": {"house": {"capacity": {"stateOfCharge": {"value": 0.46}}}}}
	}`)
	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}
	if state.ChargingCurrentA != nil || state.ChargingPowerW != nil {
		t.Fatalf("expected nil current and power when unreported, got %v %v", state.ChargingCurrentA, state.ChargingPowerW)
	}
}

func TestFetchSignalKElectricalState_RealMinusOneDischargeIsKept(t *testing.T) {
	seedSelfTree(t, `{
		"timestamp": "2026-09-02T00:00:00Z",
		"electrical": {"batteries": {"house": {
			"capacity": {"stateOfCharge": {"value": 0.46}},
			"current": {"value": -1.0},
			"power": {"value": -1.0}
		}}}
	}`)
	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}
	if state.ChargingCurrentA == nil || *state.ChargingCurrentA != -1 {
		t.Fatalf("expected a real -1 A to be kept, got %v", state.ChargingCurrentA)
	}
	if state.ChargingPowerW == nil || *state.ChargingPowerW != -1 {
		t.Fatalf("expected a real -1 W to be kept, got %v", state.ChargingPowerW)
	}
}
