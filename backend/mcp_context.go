package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// The generated boat context (ADR 0231). It tells an outside assistant what
// Mate's prompt tells Mate: how this boat is set up, in which units to speak,
// and in which time zone. It is built on every read from the same readers as
// get_vessel_status (roles, equipment items, display units, tank levels), never
// written by hand, so it cannot drift from the boat.
//
// A part that cannot be read is reported in place with the reason, and the
// other parts still appear. Nothing is filled in.

// mcpInstructions are the rules that apply outside Mate, sent as the server's
// instructions. The boat-specific facts are in the context, not here.
const mcpInstructions = `This is the read-only data server of one boat's Helmcentral. Everything it returns is read from the boat.

Read the boat context first (the resource helmcentral://boat/context, or the tool get_boat_context). It names the equipment, the roles such as the house bank, the units, and the boat's time zone.

- Give figures in the operator's units, exactly as the tools give them: quote the display value with its display_unit. Do not convert them again.
- A field that says "missing", or that has a missing reason, has no reading. Say that and give the reason. Never estimate it, and never quote an older reading in its place.
- Report alarms as worded. Do not reword an alarm's title or body, and do not say an alarm is cleared or fixed unless the result says so.
- Never guess a SignalK path. Use a path only if a tool or the context gave it to you. A device name on an alarm card is a label, not a path.
- Times are boat-local unless a result says they are UTC. Use the boat's time zone from the context when you say a time.
- Nothing here can change the boat. If asked to change something, say that this server can only read.`

const (
	mcpContextResourceURI = "helmcentral://boat/context"
)

// mcpDisplayQuantities are the SI units the boat's readings arrive in, shown
// with the unit the operator reads them in. A unit with no row here is shown as
// the boat declares it.
var mcpDisplayQuantities = []struct{ si, what string }{
	{"V", "voltage"}, {"A", "current"}, {"W", "power"}, {"m", "length and depth"},
	{"m/s", "speed"}, {"K", "temperature"}, {"Pa", "pressure"}, {"m3/s", "flow"}, {"Hz", "engine speed"}, {"ratio", "levels"},
}

// buildBoatContext writes the context for a token whose tool list is `on`. A
// part drawn from a switchable tool is left out, saying nothing, when that tool
// is off for the token, so the context is never a way round the list:
// particulars need get_vessel_particulars, alarm rules need list_alarm_rules,
// tank levels need get_vessel_status, and the other equipment items need
// find_equipment.
func buildBoatContext(d assistantToolDeps, on func(string) bool) (string, error) {
	if d.now == nil {
		return "", errors.New("get_boat_context: not available (now not wired)")
	}
	now := d.now().UTC()
	var b strings.Builder

	name := "this boat"
	var state vesselStateData
	var stateErr error
	if d.vesselState != nil {
		state, stateErr = d.vesselState()
		// Read once: the boat's time below uses this same reading.
		read, readErr := state, stateErr
		d.vesselState = func() (vesselStateData, error) { return read, readErr }
		if stateErr == nil && strings.TrimSpace(state.Name) != "" {
			name = strings.TrimSpace(state.Name)
		}
	} else {
		stateErr = errors.New("the boat's state reader is not wired")
	}
	fmt.Fprintf(&b, "# Boat context: %s\n\n", name)
	fmt.Fprintf(&b, "Generated from Helmcentral's own records at %s UTC. It is rebuilt on every read.\n\n", now.Format("2006-01-02 15:04"))

	// Time zone.
	b.WriteString("## Time zone\n\n")
	bt := d.boatTime()
	if bt.Err != nil {
		fmt.Fprintf(&b, "Not available: the boat's time zone could not be read (%v).\n\n", bt.Err)
	} else {
		fmt.Fprintf(&b, "Boat time zone: %s. Times from the tools are boat-local unless they say UTC.\n\n", bt.PromptClock(now))
	}

	// Units.
	system, unitsErr := "", error(nil)
	if d.displayUnits == nil {
		unitsErr = errors.New("the units setting reader is not wired")
	} else {
		system, unitsErr = d.displayUnits()
	}
	b.WriteString("## Units\n\n")
	if unitsErr != nil {
		fmt.Fprintf(&b, "Not available: the operator's units could not be read (%v).\n\n", unitsErr)
	} else {
		fmt.Fprintf(&b, "The operator reads **%s** units. Tool results carry the SI value and a display value in the operator's unit: quote the display value.\n\n", system)
		b.WriteString("| Quantity | The boat reports | The operator reads |\n| --- | --- | --- |\n")
		for _, q := range mcpDisplayQuantities {
			label, _, err := chartDisplayFor("", q.si, system)
			if err != nil {
				fmt.Fprintf(&b, "| %s | %s | not available: %v |\n", q.what, q.si, err)
				continue
			}
			if q.si == "Pa" {
				label = "hPa" // the barometer reads in hectopascals on both systems
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", q.what, q.si, label)
		}
		b.WriteString("\n")
	}

	// Equipment and roles.
	vessel, vesselErr := viewOf(d)
	items, itemsErr := itemsOf(d)
	b.WriteString("## Equipment and roles\n\n")
	if vesselErr != nil {
		fmt.Fprintf(&b, "Not available: the roles could not be read (%v).\n\n", vesselErr)
	} else {
		mcpWriteRoles(&b, d, vessel, items, itemsErr, on("find_equipment"))
	}

	if on("get_vessel_status") {
		b.WriteString("## Tanks\n\n")
		if itemsErr != nil {
			fmt.Fprintf(&b, "Not available: the equipment items could not be read (%v).\n\n", itemsErr)
		} else {
			mcpWriteTanks(&b, d, items)
		}
	}
	if on("get_vessel_particulars") {
		b.WriteString("## Boat particulars\n\n")
		mcpWriteParticulars(&b, d)
	}
	if on("list_alarm_rules") {
		b.WriteString("## Alarm rules\n\n")
		mcpWriteAlarmRules(&b, d)
	}

	return b.String(), nil
}

func viewOf(d assistantToolDeps) (vesselView, error) {
	if d.vesselView == nil {
		return vesselView{}, errors.New("the roles reader is not wired")
	}
	return d.vesselView()
}

func itemsOf(d assistantToolDeps) ([]sourceItem, error) {
	if d.sourceItems == nil {
		return nil, errors.New("the equipment reader is not wired")
	}
	return d.sourceItems()
}

func lastSegment(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

func mcpWriteRoles(b *strings.Builder, d assistantToolDeps, v vesselView, items []sourceItem, itemsErr error, otherItems bool) {
	if v.HouseBank == nil {
		if v.HouseBankUnresolved {
			b.WriteString("- **House bank:** a house bank item was chosen, but it has no SignalK source, so it cannot be read.\n")
		} else {
			b.WriteString("- **House bank:** no house bank role is set, so a house bank question cannot be answered until the operator chooses one.\n")
		}
	} else {
		hb := v.HouseBank
		label := "the item chosen as the house bank"
		if itemsErr == nil {
			for _, it := range items {
				if it.ID == hb.EquipmentID || (hb.EquipmentID == "" && it.Source == hb.Path) {
					label = it.Name
				}
			}
		}
		if label == "the item chosen as the house bank" {
			for _, bat := range v.Batteries {
				if bat.Instance == lastSegment(hb.Path) && bat.Name != "" {
					label = bat.Name
				}
			}
		}
		fmt.Fprintf(b, "- **House bank:** %s (`%s`)", label, hb.Path)
		if hb.CapacityAh > 0 {
			fmt.Fprintf(b, ", %g Ah", hb.CapacityAh)
		}
		if hb.Cells > 0 {
			fmt.Fprintf(b, ", %d cells", hb.Cells)
		}
		b.WriteString(".")
		if d.siUnit != nil {
			var decl []string
			for _, suffix := range []string{".capacity.stateOfCharge", ".voltage", ".current"} {
				p := hb.Path + suffix
				u := d.siUnit(p)
				if u == "" {
					decl = append(decl, fmt.Sprintf("`%s` declares no unit", suffix[1:]))
				} else {
					decl = append(decl, fmt.Sprintf("`%s` in %s", suffix[1:], u))
				}
			}
			fmt.Fprintf(b, " The boat's own unit declarations (SignalK meta): %s.", strings.Join(decl, ", "))
		}
		b.WriteString("\n")
	}
	for _, e := range v.Engines {
		fmt.Fprintf(b, "- **Engine:** %s (`propulsion.%s`).\n", e.Name, e.Instance)
	}
	if len(v.Engines) == 0 {
		b.WriteString("- **Engines:** none set up.\n")
	}
	if a := v.Alternators; a.PortInstance != "" || a.StarboardInstance != "" {
		if a.PortInstance != "" {
			fmt.Fprintf(b, "- **Port alternator:** `electrical.alternator.%s`.\n", a.PortInstance)
		}
		if a.StarboardInstance != "" {
			fmt.Fprintf(b, "- **Starboard alternator:** `electrical.alternator.%s`.\n", a.StarboardInstance)
		}
	}
	for _, s := range v.Solar {
		name := s.Name
		if name == "" {
			name = "Solar controller"
		}
		fmt.Fprintf(b, "- **Solar:** %s (`electrical.solar.%s`).\n", name, s.Instance)
	}
	for _, bat := range v.Batteries {
		if bat.Name != "" {
			fmt.Fprintf(b, "- **Battery:** %s (`electrical.batteries.%s`).\n", bat.Name, bat.Instance)
		}
	}
	if itemsErr == nil && otherItems {
		var other []string
		for _, it := range items {
			if it.Group == sourceTank {
				continue
			}
			other = append(other, fmt.Sprintf("%s (`%s`, %s)", it.Name, it.Source, it.Group))
		}
		if len(other) > 0 {
			sort.Strings(other)
			fmt.Fprintf(b, "- **Equipment items with a SignalK source:** %s.\n", strings.Join(other, "; "))
		}
	}
	b.WriteString("\n")
}

func mcpWriteTanks(b *strings.Builder, d assistantToolDeps, items []sourceItem) {
	if d.tankDefaults == nil {
		b.WriteString("Not available: the tank alarm levels reader is not wired.\n\n")
		return
	}
	defaults := d.tankDefaults()
	n := 0
	for _, it := range items {
		if it.Group != sourceTank {
			continue
		}
		n++
		kind := tankKindOfInstance(it.Instance)
		levels, off := resolveTankLevels(kind, defaults, it)
		bad := "low"
		if !tankLowIsBad(kind) {
			bad = "high"
		}
		switch {
		case off:
			fmt.Fprintf(b, "- **%s** (`%s`, %s): level alarms are switched off. A level is bad when %s.\n", it.Name, it.Source, kind, bad)
		default:
			fmt.Fprintf(b, "- **%s** (`%s`, %s): warn %g%%, alarm %g%%. A level is bad when %s.", it.Name, it.Source, kind, levels.Warn, levels.Alarm, bad)
			if perr := validateTankLevels(kind, levels); perr != nil {
				fmt.Fprintf(b, " These levels are not a valid pair, so no alarm is raised: %v.", perr)
			}
			b.WriteString("\n")
		}
	}
	if n == 0 {
		b.WriteString("No tanks are set up on the Equipment page.\n")
	}
	b.WriteString("\n")
}

func mcpWriteParticulars(b *strings.Builder, d assistantToolDeps) {
	store, err := d.recordStore("get_boat_context")
	if err != nil {
		fmt.Fprintf(b, "Not available: %v.\n\n", err)
		return
	}
	v, err := store.GetVesselParticulars()
	if err != nil {
		fmt.Fprintf(b, "Not available: the particulars could not be read (%v).\n\n", err)
		return
	}
	snap := vesselParticularsSnapshot(v)
	n := 0
	for _, l := range vesselParticularsLabels {
		if mcpContactFields[l.field] {
			continue
		}
		val := snap.Fields[l.field]
		if val == nil || val == "" {
			continue
		}
		fmt.Fprintf(b, "- %s: %v\n", l.label, val)
		n++
	}
	if n == 0 {
		b.WriteString("None recorded.\n")
	}
	b.WriteString("\nThe owner's contact and insurance details are not shared.\n\n")
}

func mcpWriteAlarmRules(b *strings.Builder, d assistantToolDeps) {
	store, err := d.recordStore("get_boat_context")
	if err != nil {
		fmt.Fprintf(b, "Not available: %v.\n\n", err)
		return
	}
	var stored []alarmRule
	if err := store.Read(func(q sqlQueryer) error {
		var err error
		stored, err = listAlarmRulesQ(q)
		return err
	}); err != nil {
		fmt.Fprintf(b, "Not available: the alarm rules could not be read (%v).\n\n", err)
		return
	}
	var lines []string
	add := func(r alarmRule, derived bool) {
		e := alarmRuleListEntry(r, derived)
		state := "on"
		if !e.Enabled {
			state = "off"
		}
		line := fmt.Sprintf("- **%s** (`%s`): %s %g", e.Label, e.Path, e.Op, e.Value)
		if e.DisplayUnit != "" {
			line += " " + e.DisplayUnit
		}
		line += fmt.Sprintf(", %s, %s", e.State, state)
		if e.Origin != "own" {
			line += ", " + e.Origin
		}
		lines = append(lines, line+".")
	}
	for _, r := range stored {
		add(r, false)
	}
	for _, r := range derivedAlarmRules() {
		add(r, true)
	}
	if len(lines) == 0 {
		b.WriteString("No alarm rules are set.\n\n")
		return
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\n")
}
