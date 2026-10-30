package main

import "log"

/*
The follow-up notification for Mate's review of an alarm (ADR 0237).

It is its own kind of message, like a watch report: it never passes through the
alarm presenter or the alarm log, never reaches the alarm banner, and never goes
to the SignalK transport, which would put Mate's words on the bus as an alarm.
It uses the rule's own channels, is queued after the alarm's notification, and
is retried by the same queue under a key no alarm transition shares, so a later
raise or clear of the rule does not drop it.
*/

const notificationKindAlarmReview = "alarm_review"

func (m notificationMessage) isAlarmReview() bool { return m.Kind == notificationKindAlarmReview }

// isReport is a message that is a report from Mate's side, not an alarm
// transition: it is never urgent and never reaches the SignalK transport, which
// would put it on the bus as an alarm.
func (m notificationMessage) isReport() bool { return m.isWatchReport() || m.isAlarmReview() }

func alarmReviewQueueKey(ruleID string) string { return "alarm-review:" + ruleID }

// enqueueAlarmReview hands a review's follow-up to run for delivery to the
// rule's channels and returns at once.
func (d *alarmDispatcher) enqueueAlarmReview(msg notificationMessage, channels []string) {
	d.enqueueItem(queuedAlarmDispatch{message: &msg, channels: channels})
}

// dispatchAlarmReview sends the follow-up on the rule's channels, which are the
// ones its alarm went to: a rule naming none uses every switched-on transport.
// SignalK is never used.
func (d *alarmDispatcher) dispatchAlarmReview(msg notificationMessage, channels []string) {
	selected := d.nonBusTransports(channels)
	if len(selected) == 0 {
		log.Printf("alarm review: %q was not sent: none of the rule's channels is switched on", msg.Label)
		return
	}
	d.sendAll(selected, msg, "alarm review", nil, nil)
}
