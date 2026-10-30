package daemon

import "time"

// A hibernated workload resumes with every connection it held dead on the far
// side, which cannot tell a frozen peer from a vanished one. Two things tell
// agynd it has been woken. The runner sends SIGCONT to the container's init
// once the guest's clock is set, twice a few seconds apart, because a signal
// in the first half second after a resume can go missing. And the clock itself
// says so: the runner sets the wall clock after every restore while the
// monotonic clock slept through the pause, so between two ticks of the
// keepalive the wall clock has moved further than the monotonic one by the
// length of the sleep. The signal makes the wake prompt; the clock makes it
// certain.

// wakeRepeatWindow is how long after a handled wake another sign of one counts
// as a repeat of it rather than a wake of its own. The runner's second signal
// follows its first by a few seconds and the clock is read once a tick; two
// ticks cover both with room for a late tick. It is measured on the wall
// clock, which unlike the monotonic one moves across a sleep, so two wakes
// with a sleep between them are never taken for one however close together
// they are in running time.
const wakeRepeatWindow = 2 * workloadKeepaliveInterval

// clockReading is one look at both clocks: the wall clock, which the runner
// sets, and the monotonic clock, which sleeps through a pause.
type clockReading struct {
	wall time.Time
	mono time.Duration
}

var clockEpoch = time.Now()

// readClocks takes both readings from a single time.Now. A Time carries both:
// Sub uses the monotonic reading when it has one, so the wall reading is kept
// apart by stripping it with Round(0). Compared as they come, the two could
// never differ.
func readClocks() clockReading {
	now := time.Now()
	return clockReading{wall: now.Round(0), mono: now.Sub(clockEpoch)}
}

// wakeWatch notices a wake from the clocks and tells a wake from the repeats
// of one. It belongs to the keepalive loop alone.
type wakeWatch struct {
	clocks  func() clockReading
	last    clockReading
	handled time.Time
}

func newWakeWatch(clocks func() clockReading) *wakeWatch {
	if clocks == nil {
		clocks = readClocks
	}
	return &wakeWatch{clocks: clocks, last: clocks()}
}

// tick reports how far the wall clock has got ahead of the monotonic clock
// since the previous tick: nothing while the workload runs, the length of the
// sleep after a restore.
func (w *wakeWatch) tick() time.Duration {
	now := w.clocks()
	jump := now.wall.Sub(w.last.wall) - (now.mono - w.last.mono)
	w.last = now
	return jump
}

// admit reports whether a sign of a wake is a wake of its own rather than a
// repeat of the last one handled, and counts it as handled when it is.
func (w *wakeWatch) admit() bool {
	now := w.clocks()
	if !w.handled.IsZero() {
		if since := now.wall.Sub(w.handled); since >= 0 && since < wakeRepeatWindow {
			return false
		}
	}
	w.handled = now.wall
	return true
}
