package netcode

import "time"

type Budget struct {
	rate, burst, tokens float64
	at                  time.Time
}

func NewBudget(rate, burst float64, now time.Time) *Budget {
	return &Budget{rate: rate, burst: burst, tokens: burst, at: now}
}
func (b *Budget) Allow(cost float64, now time.Time) bool {
	if cost < 0 {
		return false
	}
	elapsed := now.Sub(b.at).Seconds()
	if elapsed > 0 {
		b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
		b.at = now
	}
	if cost > b.tokens {
		return false
	}
	b.tokens -= cost
	return true
}
