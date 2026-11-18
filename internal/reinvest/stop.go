package reinvest

import "sync"

// stopHook is what to say if a signal ends reinvest in the middle of a step.
// A swap sets it while it is half done: the step's group is ended, this runs,
// then reinvest exits. It runs once, so a second signal cannot say it twice.
var stopHook struct {
	sync.Mutex
	say func()
}

// setStopHook sets what a stop says from now on and gives back how to unset it.
func setStopHook(say func()) (unset func()) {
	stopHook.Lock()
	stopHook.say = say
	stopHook.Unlock()
	return func() {
		stopHook.Lock()
		stopHook.say = nil
		stopHook.Unlock()
	}
}

// runStopHook says it, if there is anything to say.
func runStopHook() {
	stopHook.Lock()
	say := stopHook.say
	stopHook.say = nil
	stopHook.Unlock()
	if say != nil {
		say()
	}
}
