package main

import (
	"testing"
)

// Regression: evaluateAlarmsOnce used to return early when no rules were
// configured, but evaluate is also what drops statuses for deleted rules — so
// deleting the last rule left its alarm stuck active forever.
func TestEvaluateAlarmsOnceClearsStatusesWhenLastRuleIsDeleted(t *testing.T) {
	withTempAlarmRules(t)
	withGlobalSnapshot(t, snapshotWithSelfDelta("electrical.batteries.house.voltage", 11.0, alarmNow))

	original := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = original })

	rule := validRule()
	rule.DwellSeconds = 0
	created, err := createAlarmRule(rule)
	if err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}

	evaluateAlarmsOnce(alarmNow)
	if len(activeAlarms()) != 1 {
		t.Fatalf("expected the rule to be firing, got %d active", len(activeAlarms()))
	}

	if err := deleteAlarmRule(created.ID); err != nil {
		t.Fatalf("deleteAlarmRule: %v", err)
	}
	evaluateAlarmsOnce(alarmNow.Add(1 * 1e9))

	if got := len(activeAlarms()); got != 0 {
		t.Fatalf("deleting the last rule must clear its alarm, still %d active", got)
	}
	if worstAlarmState() != alarmStateNormal {
		t.Fatalf("worst state after deleting the last rule: got %q", worstAlarmState())
	}
}
