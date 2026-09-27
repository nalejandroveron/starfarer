package netcode

import (
	"bytes"
	"testing"
	"time"
)

func TestReliableLossDuplicationAndBackpressure(t *testing.T) {
	var sender, receiver ReliableChannel
	for i := 0; i < 64; i++ {
		if !sender.Enqueue([]byte{byte(i + 1)}) {
			t.Fatal("queue refused capacity")
		}
	}
	if sender.Enqueue([]byte{99}) {
		t.Fatal("unbounded reliable queue")
	}
	now := time.Unix(0, 0)
	executions := 0
	for i := 0; i < 64; i++ {
		frame, ok := sender.Due(now, time.Millisecond)
		if !ok {
			t.Fatal("no pending action")
		}
		accept := func(data []byte) bool { executions++; return true }
		ack := receiver.Receive(frame.Sequence, frame.Payload, accept)
		// Lose the acknowledgement and deliver a duplicate.
		now = now.Add(time.Second)
		retry, ok := sender.Due(now, time.Millisecond)
		if !ok || retry.Sequence != frame.Sequence {
			t.Fatal("retry missing")
		}
		if receiver.Receive(retry.Sequence, retry.Payload, accept) != ack {
			t.Fatal("duplicate ack mismatch")
		}
		if sender.Ack(ack+1) || !sender.Ack(ack) {
			t.Fatal("invalid acknowledgement handling")
		}
	}
	if executions != 64 || sender.Pending() != 0 {
		t.Fatal("duplicate action execution")
	}
}
func TestInterestHysteresisAndCapacity(t *testing.T) {
	grid := NewSpatialIndex(10)
	self := Point{ID: 1}
	grid.Rebuild([]Point{self, {ID: 2, X: 11}, {ID: 3, X: 9}})
	selected := grid.Select(self, 10, 12, 10, nil)
	if len(selected) != 2 || selected[1] != 3 {
		t.Fatal(selected)
	}
	selected = grid.Select(self, 10, 12, 10, map[uint64]bool{2: true})
	if len(selected) != 3 {
		t.Fatal("hysteresis lost existing member")
	}
	selected = grid.Select(self, 10, 12, 1, nil)
	if len(selected) != 1 || selected[0] != 1 {
		t.Fatal("self displaced under capacity pressure")
	}
}
func TestProtectionTamperReplayReorderAndExpiry(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sender, _ := NewProtector(key)
	receiver, _ := NewProtector(key)
	first, _ := sender.Seal([]byte("first"))
	second, _ := sender.Seal([]byte("second"))
	now := time.Now()
	if data, err := receiver.Open(second, now); err != nil || string(data) != "second" {
		t.Fatal(err)
	}
	if data, err := receiver.Open(first, now); err != nil || string(data) != "first" {
		t.Fatal("valid reordered packet lost")
	}
	if _, err := receiver.Open(first, now); err == nil {
		t.Fatal("replay accepted")
	}
	corrupt, _ := sender.Seal([]byte("secret"))
	corrupt[len(corrupt)-1] ^= 1
	if _, err := receiver.Open(corrupt, now); err == nil {
		t.Fatal("tamper accepted")
	}
	fresh, _ := sender.Seal([]byte("expired"))
	if _, err := receiver.Open(fresh, now.Add(31*time.Second)); err == nil {
		t.Fatal("expired replay accepted")
	}
	wrong, _ := NewProtector(bytes.Repeat([]byte{8}, 32))
	if _, err := wrong.Open(second, now); err == nil {
		t.Fatal("wrong key accepted")
	}
}
func TestLiveSchedulerRates(t *testing.T) {
	now := time.Unix(0, 0)
	config := DefaultConfig()
	scheduler, _ := NewScheduler(config, now)
	config.SimulationHz = 20
	config.SnapshotHz = 5
	if err := scheduler.Configure(config, now); err != nil {
		t.Fatal(err)
	}
	result := scheduler.Advance(now.Add(200 * time.Millisecond))
	if result.Steps != 4 || !result.Snapshot {
		t.Fatal("live rates not applied")
	}
}
