package rules

import "time"

// Streak is a rule's pending action on one owner and instance: the action
// its samples have pointed at, without a break, since Since. Since is the
// server's clock when the streak began (the receipt of the sample that
// started it), never a sample's own timestamp, so a node's clock skew
// neither waives nor stretches a sustain. Hold is no streak. Durable, so a
// sustain survives a restart.
type Streak struct {
	Pending Action
	Since   time.Time
}

func sustainFor(a Action, fire, clear time.Duration) time.Duration {
	if a == Fire {
		return fire
	}
	return clear
}

// Due reports whether the streak has held for its action's sustain at now.
func (s Streak) Due(now time.Time, fire, clear time.Duration) bool {
	if s.Pending == Hold {
		return false
	}
	return !now.Before(s.Since.Add(sustainFor(s.Pending, fire, clear)))
}

// Step advances a streak by one sample's decision, received at now on the
// server's clock: a hold breaks any streak; an action continues its streak
// (or starts one at now) and takes effect once it has held for its sustain
// (at once when the sustain is zero). The returned effect is the action to
// apply now, Hold for none; once an action takes effect the streak ends.
func Step(prev Streak, action Action, now time.Time, fire, clear time.Duration) (Streak, Action) {
	if action == Hold {
		return Streak{}, Hold
	}
	s := Streak{Pending: action, Since: now}
	if prev.Pending == action && prev.Since.Before(now) {
		s.Since = prev.Since
	}
	if s.Due(now, fire, clear) {
		return Streak{}, action
	}
	return s, Hold
}
