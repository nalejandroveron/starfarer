package netcode

// InputBuffer holds bounded, ordered commands. It does not decide whether a
// missing command should be skipped, replaced, or trigger resynchronization.
type InputBuffer[T any] struct {
	capacity uint64
	pending  map[uint64]T
}

func NewInputBuffer[T any](capacity uint64) *InputBuffer[T] {
	if capacity == 0 {
		panic("input capacity must be positive")
	}
	return &InputBuffer[T]{capacity: capacity, pending: make(map[uint64]T)}
}

func (b *InputBuffer[T]) Add(sequence, resolved uint64, value T) bool {
	if sequence <= resolved || sequence-resolved > b.capacity {
		return false
	}
	if _, exists := b.pending[sequence]; exists {
		return false
	}
	b.pending[sequence] = value
	return true
}

func (b *InputBuffer[T]) TakeNext(resolved uint64) (T, bool) {
	if resolved == ^uint64(0) {
		var zero T
		return zero, false
	}
	sequence := resolved + 1
	value, exists := b.pending[sequence]
	if exists {
		delete(b.pending, sequence)
	}
	return value, exists
}

func (b *InputBuffer[T]) Len() int { return len(b.pending) }
