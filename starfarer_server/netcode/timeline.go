package netcode

// Timeline resolves every authoritative tick, including missing input. Applied
// identifies an actual command; Cursor also includes fallback/neutral steps.
type Timeline[T any] struct {
	Cursor, Applied, Skipped uint64
	window, hold             uint64
	pending                  map[uint64]T
	last                     T
}

func NewTimeline[T any](cursor, window, hold uint64) *Timeline[T] {
	if window == 0 {
		panic("empty timeline window")
	}
	return &Timeline[T]{Cursor: cursor, window: window, hold: hold, pending: make(map[uint64]T)}
}
func (t *Timeline[T]) Add(tick uint64, value T) bool {
	if tick <= t.Cursor || tick-t.Cursor > t.window {
		return false
	}
	if _, ok := t.pending[tick]; ok {
		return false
	}
	t.pending[tick] = value
	return true
}
func (t *Timeline[T]) Advance(target, maxSteps uint64, simulate func(T)) {
	if target <= t.Cursor {
		return
	}
	if maxSteps == 0 {
		panic("zero catch-up budget")
	}
	if target-t.Cursor > maxSteps {
		t.Skipped += target - t.Cursor - maxSteps
		t.Cursor = target - maxSteps
		var zero T
		t.last = zero
		t.Applied = 0
		for tick := range t.pending {
			if tick <= t.Cursor {
				delete(t.pending, tick)
			}
		}
	}
	for t.Cursor < target {
		t.Cursor++
		if value, ok := t.pending[t.Cursor]; ok {
			t.last = value
			t.Applied = t.Cursor
			delete(t.pending, t.Cursor)
		}
		value := t.last
		if t.Applied == 0 || t.Cursor-t.Applied > t.hold {
			var zero T
			value = zero
		}
		simulate(value)
	}
}
func (t *Timeline[T]) ClearPending() { clear(t.pending); var zero T; t.last = zero; t.Applied = 0 }
func (t *Timeline[T]) Len() int      { return len(t.pending) }
