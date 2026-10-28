package main

// builtInAlarmCheckIDs is every alarm id that Helmcentral's own code raises
// with no stored rule behind it. get_alarm_context reads it to tell such a
// check from a rule the operator has since deleted. Add an id here when a new
// built-in check starts raising alarms.
var builtInAlarmCheckIDs = map[string]bool{
	collisionProfileRuleID: true,
	watchdogRuleID:         true,
	anchorWatchStateRuleID: true,
	anchorDragRuleID:       true,
}

func isBuiltInAlarmCheck(ruleID string) bool {
	return builtInAlarmCheckIDs[ruleID]
}
