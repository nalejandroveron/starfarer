package netcode

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestInputBufferBoundariesAndMissingSequence(t *testing.T) {
	b := NewInputBuffer[string](2)
	if b.Add(0, 0, "zero") || b.Add(3, 0, "ahead") {
		t.Fatal("out-of-window input accepted")
	}
	if !b.Add(2, 0, "second") {
		t.Fatal("valid reordered input rejected")
	}
	if _, ok := b.TakeNext(0); ok {
		t.Fatal("missing sequence skipped")
	}
	if !b.Add(1, 0, "first") || b.Add(1, 0, "changed") {
		t.Fatal("duplicate handling failed")
	}
	if value, ok := b.TakeNext(0); !ok || value != "first" {
		t.Fatal("first input changed")
	}
	if value, ok := b.TakeNext(1); !ok || value != "second" {
		t.Fatal("second input lost")
	}
	if b.Len() != 0 || b.Add(1, 2, "old") {
		t.Fatal("acknowledged input replayed")
	}
	if _, ok := b.TakeNext(^uint64(0)); ok {
		t.Fatal("sequence wrapped")
	}
}

func TestSchedulerSeparatesRatesAndBoundsCatchUp(t *testing.T) {
	start := time.Unix(0, 0)
	config := DefaultConfig()
	config.SimulationHz = 20
	config.SnapshotHz = 4
	s, err := NewScheduler(config, start)
	if err != nil {
		t.Fatal(err)
	}
	first := s.Advance(start.Add(100 * time.Millisecond))
	if first.Steps != 2 || first.Snapshot {
		t.Fatalf("unexpected first advance: %+v", first)
	}
	next := s.Advance(start.Add(250 * time.Millisecond))
	if next.Steps != 3 || !next.Snapshot || s.Tick != 5 {
		t.Fatal("snapshot rate tied to receive loop")
	}
	stalled := s.Advance(start.Add(2 * time.Second))
	if stalled.Steps != 5 || !stalled.Snapshot || s.DroppedSteps == 0 {
		t.Fatal("unbounded backlog")
	}
	if retry := s.Advance(start.Add(2 * time.Second)); retry.Steps != 0 || retry.Snapshot {
		t.Fatal("burst repeated after stall")
	}
	for _, c := range []Config{{}, {SimulationHz: 241, SnapshotHz: 10, MaxCatchUpSteps: 5, MaxDatagramBytes: 1200}} {
		if _, err := NewScheduler(c, start); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}

func TestUDPHostRejectsOversizedPacketAndContinues(t *testing.T) {
	config := DefaultConfig()
	host, err := ListenUDP("127.0.0.1:0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan string, 1)
	rejected := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- host.Run(ctx, HostCallbacks{
			Receive: func(_ *net.UDPAddr, data []byte, _ time.Time) { received <- string(data); cancel() },
			Report: func(err error) {
				select {
				case rejected <- err:
				default:
				}
			},
		})
	}()
	conn, err := net.DialUDP("udp", nil, host.Address().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.Write(make([]byte, 1201)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rejected:
	case <-time.After(time.Second):
		t.Fatal("oversized packet not rejected")
	}
	if _, err = conn.Write([]byte("valid")); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-received:
		if value != "valid" {
			t.Fatal(value)
		}
	case <-time.After(time.Second):
		t.Fatal("host stopped receiving")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("host did not cancel")
	}
}
