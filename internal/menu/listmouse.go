package menu

import "github.com/thousandflowers/skillreaper/internal/term"

// paintRow is a click or a drag over a line: it sets reap's own choice on it, or
// clears exactly that.
func (l *list) paintRow(idx int, on bool) {
	for _, r := range groupAll(l.flat[idx]) {
		choice := best(r)
		if choice == "" || l.m.applied[r] != "" {
			continue
		}
		switch {
		case on && l.p.get(r) != choice:
			l.p.set(r, choice)
			l.p.sweep[r] = 0
		case !on && l.p.get(r) == choice:
			l.p.pop(r)
		}
	}
}

func (l *list) mouse(ev term.Mouse) {
	idx, onRow := l.rowmap[ev.Y]
	l.repeat = nil
	switch {
	case ev.Kind == "release":
		l.forgetPaint()
	case ev.Kind == "wheel_up" || ev.Kind == "wheel_down":
		// The wheel moves the view, and the cursor only as far as it must to stay in it.
		step := wheelRows
		if ev.Kind == "wheel_up" {
			step = -wheelRows
		}
		l.heldTop = max(0, min(l.top+step, max(0, len(l.flat)-l.view)))
		l.i = max(l.heldTop, min(l.i, l.heldTop+l.view-1))
		l.endSelection()
	case ev.Kind == "press" && onRow && l.flat[idx].isGroup && ev.X >= 6 && ev.X <= 8:
		// Around a group's triangle: a click here opens it.
		l.heldTop, l.i, l.msg = l.top, idx, ""
		l.endSelection()
		l.opened[l.flat[idx].name] = !l.opened[l.flat[idx].name]
	case ev.Kind == "press" && onRow:
		// The list holds still under the pointer, so a second click lands on the row the first one changed.
		l.heldTop, l.i, l.msg = l.top, idx, ""
		l.endSelection()
		set := false
		for _, r := range groupAll(l.flat[idx]) {
			if choice := best(r); choice != "" && l.p.get(r) != choice {
				set = true
			}
		}
		l.paint = 0
		if set {
			l.paint = 1
		}
		l.paintRow(idx, set)
		l.painted = map[int]bool{idx: true}
		l.lastPainted = idx
	case ev.Kind == "drag" && l.paint >= 0 && onRow && l.lastPainted >= 0:
		// Reports land where the pointer is: every row between two of them counts. Group headers only by a press.
		for between := min(l.lastPainted, idx); between <= max(l.lastPainted, idx) && between < len(l.flat); between++ {
			if !l.flat[between].isGroup {
				l.paintRow(between, l.paint == 1)
				l.painted[between] = true
			}
		}
		l.i, l.lastPainted = idx, idx
		l.heldTop = pointerTop(l.top, ev.Y, listY, l.view, len(l.flat))
	}
}
