package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// legacyImportMarker is recorded in alarm_rule_seeded_sets by the import's own
// transaction. It sits beside the seed markers because it means the same thing,
// that something was offered once and must not be offered again: a file put back
// after a lost rename is not imported a second time, so it cannot bring back
// rules the operator deleted since.
const legacyImportMarker = "legacy-import:alarm-rules.json"

// legacyAlarmRulesImportedSuffix is appended to the old rules file once its
// contents are in the database.
const legacyAlarmRulesImportedSuffix = ".imported"

// legacyAlarmRulesFile is alarm-rules.json as releases before ADR 0224 wrote
// it. ignored_sensors is read only to make sure the earlier move (ADR 0207) has
// already taken it, since the rename below would otherwise bury it.
type legacyAlarmRulesFile struct {
	Rules          []*alarmRule `json:"rules"`
	SeededSets     []string     `json:"seeded_sets"`
	IgnoredSensors []string     `json:"ignored_sensors"`
}

// importLegacyAlarmRules moves the rules and seeded-set markers in the old
// alarm-rules.json into helmcentral.sqlite in one transaction, then renames the
// file to alarm-rules.json.imported. It returns how many rules the file held.
//
// Like moveLegacyIgnoredSensors, the insert commits before the file moves and a
// rule already in the database is left as it is, so a crash between the two
// repeats the import on the next boot without overwriting later edits. A file
// that does not parse, or holds a rule the engine would not have accepted,
// fails the boot and is left untouched: it is the operator's only copy.
//
// Call it after moveLegacyIgnoredSensors and before loadAlarmRules.
func importLegacyAlarmRules(s *documentStore, path string, now time.Time) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading alarm rules for the import into the database: %w", err)
	}
	var file legacyAlarmRulesFile
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &file); err != nil {
			return 0, fmt.Errorf("parsing %s for the import into the database: %w", path, err)
		}
	}
	for _, id := range file.IgnoredSensors {
		if strings.TrimSpace(id) == "" {
			continue
		}
		return 0, fmt.Errorf("%s still holds an ignored-sensor list; it moves to the database before the rules do", path)
	}
	if err := validateLegacyAlarmRules(path, file.Rules); err != nil {
		return 0, err
	}
	imported := path + legacyAlarmRulesImportedSuffix
	if _, err := os.Stat(imported); err == nil {
		return 0, fmt.Errorf("%s already exists, so %s was not imported: move one of them out of the way", imported, path)
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("checking %s: %w", imported, err)
	}

	origins := inferLegacySeededBy(file.Rules, file.SeededSets)
	err = s.writeAlarmRules("import alarm rules", func(tx *sql.Tx, _ time.Time) error {
		done, err := alarmSeededSetsQ(tx)
		if err != nil {
			return err
		}
		if containsString(done, legacyImportMarker) {
			return nil // an earlier boot imported this file; only the rename is left
		}
		for _, rule := range file.Rules {
			if _, err := alarmRuleByIDQ(tx, rule.ID); err == nil {
				continue
			} else if !errors.Is(err, errAlarmRuleNotFound) {
				return err
			}
			r := *rule
			r.SeededBy = origins[r.ID]
			if r.CreatedAt.IsZero() {
				r.CreatedAt = now
			}
			if r.UpdatedAt.IsZero() {
				r.UpdatedAt = r.CreatedAt
			}
			if err := insertAlarmRuleTx(tx, r); err != nil {
				return err
			}
		}
		for _, marker := range file.SeededSets {
			if marker = strings.TrimSpace(marker); marker != "" {
				if err := recordAlarmSeededSetTx(tx, marker, now); err != nil {
					return err
				}
			}
		}
		return recordAlarmSeededSetTx(tx, legacyImportMarker, now)
	})
	if err != nil {
		return 0, fmt.Errorf("importing %s: %w", path, err)
	}
	if err := os.Rename(path, imported); err != nil {
		return 0, fmt.Errorf("the alarm rules are in the database, but %s could not be renamed: %w", path, err)
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return 0, fmt.Errorf("syncing the folder after renaming %s: %w", path, err)
		}
	}
	log.Printf("alarm rules: imported %d rule(s) and %d seeded set(s) from %s into helmcentral.sqlite; the file is now %s",
		len(file.Rules), len(file.SeededSets), path, imported)
	return len(file.Rules), nil
}

func validateLegacyAlarmRules(path string, rules []*alarmRule) error {
	seen := map[string]bool{}
	for i, rule := range rules {
		switch {
		case rule == nil || strings.TrimSpace(rule.ID) == "":
			return fmt.Errorf("%s: rule %d has no id", path, i+1)
		case seen[rule.ID]:
			return fmt.Errorf("%s: id %q appears twice", path, rule.ID)
		case strings.TrimSpace(rule.Label) == "":
			return fmt.Errorf("%s: rule %q has no label", path, rule.ID)
		case !alarmOperators[rule.Op]:
			return fmt.Errorf("%s: rule %q has unknown operator %q", path, rule.Label, rule.Op)
		case rule.State == alarmStateNormal:
			return fmt.Errorf("%s: rule %q has the cleared state", path, rule.Label)
		}
		if _, ok := alarmStateRank[rule.State]; !ok {
			return fmt.Errorf("%s: rule %q has unknown state %q", path, rule.Label, rule.State)
		}
		seen[rule.ID] = true
	}
	return nil
}

var anomalyEngineRuleID = regexp.MustCompile(`^helmcentral:anomaly-(?:coolant-temperature|oil-pressure|boost-pressure|engine-load)-(.+)-(?:high|low)$`)

// inferLegacySeededBy says which built-in set made which rule in the old file,
// as a map of rule id to marker. A rule missing from the map is the operator's.
//
//   - Anomaly rules carry fixed ids, so the id settles it.
//   - Heavy weather, forecast warnings and Law of Storms rules were given
//     random ids, so they are matched on label, path and operator against the
//     seeders' own definitions, and only for a set whose marker the file
//     records. A definition that two sets share, or that two rules in the file
//     match, is left unclaimed: guessing wrong would one day offer to reset an
//     operator's own rule. A rule the operator renamed is unclaimed too.
func inferLegacySeededBy(rules []*alarmRule, markers []string) map[string]string {
	recorded := map[string]bool{}
	for _, m := range markers {
		recorded[strings.TrimSpace(m)] = true
	}
	out := map[string]string{}

	type identity struct{ label, path, op string }
	definers := map[identity][]string{}
	for marker, defs := range map[string][]alarmRule{
		heavyWeatherSeedMarker:     heavyWeatherSeedRules(time.Now()),
		forecastWarningsSeedMarker: forecastWarningsSeedRules(),
		lawOfStormsSeedMarker:      lawOfStormsSeedRules(),
	} {
		if !recorded[marker] {
			continue
		}
		for _, d := range defs {
			key := identity{d.Label, d.Path, d.Op}
			definers[key] = append(definers[key], marker)
		}
	}
	holders := map[identity]int{}
	for _, r := range rules {
		holders[identity{r.Label, r.Path, r.Op}]++
	}

	for _, r := range rules {
		if strings.HasPrefix(r.ID, "helmcentral:anomaly-") {
			switch {
			case strings.HasPrefix(r.ID, anomalyEngineStumbleRuleID("")):
				out[r.ID] = anomalyEngineStumbleSeedMarker(strings.TrimPrefix(r.ID, anomalyEngineStumbleRuleID("")))
			case r.ID == anomalyImpossibleRuleID || r.ID == anomalySilentSourceRuleID:
				out[r.ID] = anomalyBaseSeedMarker
			case r.ID == anomalyFrozenRuleID:
				out[r.ID] = anomalyFrozenSeedMarker()
			case r.ID == anomalyFullBankWarnRuleID || r.ID == anomalyFullBankAlarmRuleID:
				out[r.ID] = anomalyBatterySeedMarker()
			default:
				if m := anomalyEngineRuleID.FindStringSubmatch(r.ID); m != nil {
					out[r.ID] = anomalyEngineSeedMarker(m[1])
				}
			}
			continue
		}
		key := identity{r.Label, r.Path, r.Op}
		if d := definers[key]; len(d) == 1 && holders[key] == 1 {
			out[r.ID] = d[0]
		}
	}
	return out
}
