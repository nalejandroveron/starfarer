package world

import (
	"math"
	"testing"
)

func TestMissingInputResolvesWithoutBlocking(t *testing.T) {
	p := NewPlayer(1)
	if !p.ApplyInput(1, 1, 0) || !p.ApplyInput(10, 0, 1) {
		t.Fatal("valid inputs rejected")
	}
	p.Advance(10)
	if p.Timeline.Cursor != 10 || p.Timeline.Applied != 10 {
		t.Fatal("missing sequence blocked frontier")
	}
	if math.Abs(float64(p.X)-7.0*5/60) > 1e-5 || math.Abs(float64(p.Z)-5.0/60) > 1e-5 {
		t.Fatalf("fallback was not bounded: %+v", p)
	}
	if p.ApplyInput(2, 1, 0) {
		t.Fatal("late input rewrote history")
	}
}
func TestInputCannotPurchaseTime(t *testing.T) {
	p := NewPlayer(1)
	for tick := uint64(1); tick <= 256; tick++ {
		p.ApplyInput(tick, 100, 100)
	}
	p.Advance(1)
	if math.Abs(math.Hypot(float64(p.X), float64(p.Z))-5.0/60) > 1e-5 {
		t.Fatal("input flood accelerated movement")
	}
	if p.ApplyInput(258, 1, 0) || p.ApplyInput(2, 1, 0) {
		t.Fatal("window or duplicate accepted")
	}
	for _, value := range []float32{float32(math.NaN()), float32(math.Inf(1))} {
		if p.ApplyInput(257, value, 0) {
			t.Fatal("non-finite accepted")
		}
	}
}
func TestCallbackRatesProduceSameMovement(t *testing.T) {
	for _, hz := range []int{7, 20, 30, 60, 120} {
		p := NewPlayer(1)
		for tick := uint64(1); tick <= 120; tick++ {
			p.ApplyInput(tick, 0.6, 0.8)
		}
		for i := 1; i <= hz*2; i++ {
			p.Advance(uint64(i * 60 / hz))
		}
		if math.Abs(float64(p.X)-6) > 1e-4 || math.Abs(float64(p.Z)-8) > 1e-4 {
			t.Fatalf("rate %d changed speed: %v,%v", hz, p.X, p.Z)
		}
	}
}
func TestLongPauseBoundsWorkAndClearsOldInputs(t *testing.T) {
	p := NewPlayer(1)
	p.ApplyInput(1, 1, 0)
	p.Advance(600)
	if p.Timeline.Cursor != 600 || p.Timeline.Skipped != 480 || p.X != 0 || p.Timeline.Len() != 0 {
		t.Fatal("unbounded replay after pause")
	}
	if !p.ApplyInput(601, 0, 1) {
		t.Fatal("timeline did not recover")
	}
	p.Advance(601)
	if p.Z <= 0 {
		t.Fatal("new input not applied after outage")
	}
}
