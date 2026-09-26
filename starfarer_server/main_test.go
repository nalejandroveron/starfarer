package main

import (
	"net"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"starfarer/internal/protocol"
	pb "starfarer/internal/protocol/v1"
)

func TestSessionIsolationAndHelloRetry(t *testing.T) {
	s := &server{sessions: make(map[string]*session)}
	a := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50001}
	b := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50002}
	now := time.Now()
	hello := &pb.ClientEvents{ProtocolVersion: 1, Event: &pb.ClientEvents_Hello{Hello: &pb.Hello{ClientVersion: "test"}}}
	input := &pb.ClientEvents{ProtocolVersion: 1, Event: &pb.ClientEvents_PlayerInput{PlayerInput: &pb.PlayerInput{Sequence: 42, MoveX: 1}}}
	if s.handle(a, input, now) != nil || len(s.sessions) != 0 {
		t.Fatal("input from unregistered peer created a session or response")
	}
	first := s.handle(a, hello, now).GetWelcome().PlayerId
	if s.handle(a, hello, now).GetWelcome().PlayerId != first {
		t.Fatal("hello retry created new session")
	}
	if s.handle(b, hello, now).GetWelcome().PlayerId == first {
		t.Fatal("clients share player ID")
	}
	player := s.sessions[a.String()].player
	acceptedAt := now.Add(time.Second)
	if s.handle(a, input, acceptedAt) != nil {
		t.Fatal("input should not send an immediate response")
	}
	if player.LastReceivedInput != 42 || player.MoveX != 1 || !s.sessions[a.String()].lastSeen.Equal(acceptedAt) {
		t.Fatal("input was not applied to its player's session")
	}
	if player.LastProcessedInput != 0 || player.X != 0 {
		t.Fatal("receiving input must not advance simulation")
	}
	if s.sessions[b.String()].player.LastReceivedInput != 0 {
		t.Fatal("input affected another player's session")
	}
	s.handle(a, input, now.Add(2*time.Second))
	if !s.sessions[a.String()].lastSeen.Equal(acceptedAt) {
		t.Fatal("duplicate input refreshed session")
	}
	s.handle(a, hello, now.Add(3*time.Second))
	if s.sessions[a.String()].player != player {
		t.Fatal("hello retry replaced player state")
	}
	s.handle(a, input, now.Add(19*time.Second))
	if _, exists := s.sessions[a.String()]; exists {
		t.Fatal("expired session accepted input")
	}
}

func TestSnapshotIncludesActivePlayersAndExpiresSessions(t *testing.T) {
	s := &server{sessions: make(map[string]*session)}
	now := time.Now()
	hello := &pb.ClientEvents{ProtocolVersion: 1, Event: &pb.ClientEvents_Hello{Hello: &pb.Hello{ClientVersion: "test"}}}
	for i := 0; i < 3; i++ {
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50001 + i}
		s.handle(address, hello, now)
		current := s.sessions[address.String()]
		current.player.X = float32(i + 1)
		current.player.Z = -float32(i + 1)
		current.player.LastProcessedInput = uint64(10 + i)
		if i == 2 {
			current.lastSeen = now.Add(-16 * time.Second)
		}
	}
	snapshot := s.snapshot(123, now)
	event := &pb.ServerEvents{ProtocolVersion: 1, Event: &pb.ServerEvents_Snapshot{Snapshot: snapshot}}
	data, err := protocol.EncodeServerEvents(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded := new(pb.ServerEvents)
	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.GetSnapshot()
	if got == nil || got.Tick != 123 || len(got.Players) != 2 || len(s.sessions) != 2 {
		t.Fatalf("expected two active players at tick 123, got %v", got)
	}
	seen := make(map[uint64]bool)
	for _, player := range got.Players {
		if player.PlayerId < 1 || player.PlayerId > 2 || seen[player.PlayerId] {
			t.Fatalf("unexpected or duplicate player: %v", player)
		}
		seen[player.PlayerId] = true
		if player.PositionX != float32(player.PlayerId) || player.PositionZ != -float32(player.PlayerId) || player.LastProcessedInput != 9+player.PlayerId {
			t.Fatalf("incorrect player state: %v", player)
		}
	}
	// A snapshot owns copies of the state, not references to mutable players.
	for _, current := range s.sessions {
		current.player.X = 99
	}
	for _, player := range snapshot.Players {
		if player.PositionX == 99 {
			t.Fatal("snapshot changed after player mutation")
		}
	}
}

func TestEnvelopeValidation(t *testing.T) {
	for name, input := range map[string][]byte{
		"empty":         nil,
		"malformed":     {0xff},
		"missing_event": {8, 1},
		"unknown_event": {8, 1, 0xa2, 6, 0},
		"oversized":     make([]byte, protocol.MaxDatagramSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := protocol.DecodeClientEvents(input); err == nil {
				t.Fatal("invalid envelope accepted")
			}
		})
	}
	message := &pb.ClientEvents{ProtocolVersion: 2, Event: &pb.ClientEvents_Hello{Hello: &pb.Hello{}}}
	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.DecodeClientEvents(data); err == nil {
		t.Fatal("unsupported version accepted")
	}
	message.ProtocolVersion = 1
	data, err = proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.DecodeClientEvents(data); err != nil {
		t.Fatal(err)
	}
}
