package main

import (
	"log"
	"strings"
)

// The Engine Cluster v2 slots that are not the ring, centre and corners: two
// arcs in the gap at the foot of the dial and a footer of cards. Every one is a
// gauge slot on the equipment model (ADR 0185): a suffix under an item's
// source, with zones from that item's profile. Arcs and the transmission and
// charge figures read the cluster's own engine; the starter battery, the
// alternator and the exhaust name their own item (dashboardGaugeConfig.EquipmentID).
// A slot that is not bound is absent.

var validClusterSides = map[string]bool{"": true, "port": true, "stbd": true}

type dashboardClusterArcs struct {
	// Left is the engine coolant; Right the gearbox oil temperature.
	Left  *dashboardGaugeConfig `json:"left,omitempty"`
	Right *dashboardGaugeConfig `json:"right,omitempty"`
}

func (a *dashboardClusterArcs) slots() []*dashboardGaugeConfig {
	if a == nil {
		return []*dashboardGaugeConfig{nil, nil}
	}
	return []*dashboardGaugeConfig{a.Left, a.Right}
}

// dashboardClusterFooter's field order fixes the derived alarm ids of held
// zones (clusterFooterZoneIndexBase + position) and may only be appended to.
type dashboardClusterFooter struct {
	Battery       *dashboardGaugeConfig `json:"battery,omitempty"`
	AltAmps       *dashboardGaugeConfig `json:"altAmps,omitempty"`
	AltWatts      *dashboardGaugeConfig `json:"altWatts,omitempty"`
	AltTemp       *dashboardGaugeConfig `json:"altTemp,omitempty"`
	AltStage      *dashboardGaugeConfig `json:"altStage,omitempty"`
	AltLimit      *dashboardGaugeConfig `json:"altLimit,omitempty"`
	TransPressure *dashboardGaugeConfig `json:"transPressure,omitempty"`
	Egt           *dashboardGaugeConfig `json:"egt,omitempty"`
}

func (f *dashboardClusterFooter) slots() []*dashboardGaugeConfig {
	if f == nil {
		return make([]*dashboardGaugeConfig, clusterFooterSlotCount)
	}
	return []*dashboardGaugeConfig{
		f.Battery, f.AltAmps, f.AltWatts, f.AltTemp, f.AltStage, f.AltLimit,
		f.TransPressure, f.Egt,
	}
}

// validateClusterSlot is validateSlotConfig plus the per-slot item.
func validateClusterSlot(g dashboardGaugeConfig, hasItem bool, ctx string) string {
	if len(g.EquipmentID) > equipmentIDMaxLen {
		return "slot equipment id too long: " + ctx
	}
	if g.ZoneIndex != nil && (*g.ZoneIndex < 0 || *g.ZoneIndex > 255) {
		return "slot zone index out of range: " + ctx
	}
	// A slot that names its own item does not need the tile's.
	return validateSlotConfig(g, hasItem || g.EquipmentID != "", ctx)
}

// moveLegacyTelltales moves what a cluster held as telltales onto the arcs: the
// engine temperature to the left arc, the gearbox temperature to the right. Anything else (the exhaust read from another
// node) is dropped and logged, not guessed at. It reports whether it changed
// anything, and is a no-op once the field is empty.
func moveLegacyTelltales(cl *dashboardClusterConfig) bool {
	if len(cl.Telltales) == 0 {
		return false
	}
	for i, t := range cl.Telltales {
		t := t
		// The zones a telltale held were issued ids by its old position. The
		// slot keeps that id wherever it lands.
		index := clusterTelltaleZoneIndexBase + i
		t.ZoneIndex = &index
		switch strings.TrimSpace(t.Suffix) {
		case "temperature":
			if cl.Arcs == nil {
				cl.Arcs = &dashboardClusterArcs{}
			}
			if cl.Arcs.Left == nil {
				cl.Arcs.Left = &t
			}
		case "transmission.oilTemperature":
			if cl.Arcs == nil {
				cl.Arcs = &dashboardClusterArcs{}
			}
			if cl.Arcs.Right == nil {
				cl.Arcs.Right = &t
			}
		default:
			log.Printf("dashboard: cluster %q: telltale %q has no place on the v2 card and was dropped", cl.Title, firstNonEmpty(t.Suffix, t.Path, t.Label))
		}
	}
	cl.Telltales = nil
	return true
}

// normalizeClusterCaps brings a cluster saved under the old caps (a corner of
// several rows, a rail of up to four tanks) within the new ones, so a page that
// holds one is still editable. What is dropped is logged. It reports whether it
// changed anything.
func normalizeClusterCaps(page string, cl *dashboardClusterConfig) bool {
	changed := false
	for ci := range cl.Corners {
		if rows := cl.Corners[ci].Rows; len(rows) > clusterMaxCornerRows {
			for _, dropped := range rows[clusterMaxCornerRows:] {
				log.Printf("dashboard: %s, cluster %q: corner %q holds one reading now; dropped %q",
					page, cl.Title, cl.Corners[ci].Label, firstNonEmpty(dropped.Suffix, dropped.Path, dropped.Label))
			}
			cl.Corners[ci].Rows = rows[:clusterMaxCornerRows]
			changed = true
		}
	}
	if cl.Fuel != nil && len(cl.Fuel.Bars) > clusterMaxFuelBars {
		for _, dropped := range cl.Fuel.Bars[clusterMaxFuelBars:] {
			log.Printf("dashboard: %s, cluster %q: the fuel fan draws two tanks; dropped %q",
				page, cl.Title, firstNonEmpty(dropped.Label, dropped.TankID, dropped.Source))
		}
		cl.Fuel.Bars = cl.Fuel.Bars[:clusterMaxFuelBars]
		changed = true
	}
	return changed
}
