package main

import (
	"bytes"
	"google.golang.org/protobuf/proto"
	"math/rand"
	"net"
	"starfarer/internal/protocol"
	pb "starfarer/internal/protocol/v1"
	"starfarer/netcode"
	"testing"
	"time"
)

func testHello(s *server, port int, now time.Time) *session {
	address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	connection := make([]byte, 16)
	connection[0] = byte(port)
	connection[1] = byte(port >> 8)
	response := s.handle(address, &pb.ClientEvents{Event: &pb.ClientEvents_Hello{Hello: &pb.Hello{ConnectionId: connection, Attempt: 1}}}, now)
	if response == nil {
		panic("hello failed")
	}
	return s.sessions[string(response.GetWelcome().ResumeToken)]
}
func command(current *session, inputs ...*pb.PlayerInput) *pb.ClientEvents {
	return &pb.ClientEvents{ProtocolVersion: protocol.Version, SessionEpoch: current.epoch, ResumeToken: current.token, Event: &pb.ClientEvents_PlayerInputs{PlayerInputs: &pb.PlayerInputBatch{Inputs: inputs}}}
}
func TestSessionResumeAndStaleEpoch(t *testing.T) {
	now := time.Now()
	s := newServer(netcode.DefaultConfig(), now, 100)
	a := testHello(s, 50001, now)
	initialEpoch := a.epoch
	b := testHello(s, 50002, now)
	if a.id == b.id {
		t.Fatal("shared identity")
	}
	s.handle(a.address, command(a, &pb.PlayerInput{Sequence: 1, MoveX: 1}), now)
	s.advance(now.Add(time.Second/60 + time.Nanosecond))
	old := command(a, &pb.PlayerInput{Sequence: 2, MoveX: 1})
	oldAddress := a.address
	connection := bytes.Repeat([]byte{77}, 16)
	address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60001}
	hello := &pb.ClientEvents{Event: &pb.ClientEvents_Hello{Hello: &pb.Hello{ConnectionId: connection, ResumeToken: a.token, Attempt: 2}}}
	resumed := s.handle(address, hello, now.Add(time.Second)).GetWelcome()
	if resumed.PlayerId != a.id || resumed.SessionEpoch != initialEpoch+1 || resumed.Player.PositionX == 0 {
		t.Fatal("resume lost state")
	}
	if s.handle(address, hello, now).GetWelcome().SessionEpoch != initialEpoch+1 {
		t.Fatal("retry changed epoch")
	}
	s.handle(oldAddress, old, now)
	if a.player.Timeline.Len() != 0 {
		t.Fatal("old epoch controlled player")
	}
	if s.handle(address, &pb.ClientEvents{Event: &pb.ClientEvents_Hello{Hello: &pb.Hello{ConnectionId: bytes.Repeat([]byte{33}, 16), ResumeToken: a.token, Attempt: 1}}}, now) != nil {
		t.Fatal("old attempt rolled back connection")
	}
	if b.player.X != 0 {
		t.Fatal("cross-session movement")
	}
}
func TestAOIAndFragmentedCompleteBaseline(t *testing.T) {
	now := time.Now()
	s := newServer(netcode.DefaultConfig(), now, 100)
	observer := testHello(s, 50000, now)
	for i := 1; i < 65; i++ {
		current := testHello(s, 50000+i, now)
		current.player.X = float32(i)
	}
	distant := testHello(s, 50100, now)
	distant.player.X = 1000
	s.advance(now.Add(time.Second))
	parts := []*pb.WorldSnapshot{}
	s.snapshots(now.Add(time.Second), func(address *net.UDPAddr, event *pb.ServerEvents) {
		if data, err := protocol.EncodeServerEvents(event); err != nil || len(data) > netcode.MaxDatagramSize {
			t.Fatalf("oversize part: %v", err)
		}
		if address.String() == observer.address.String() {
			parts = append(parts, event.GetSnapshot())
		}
	})
	count := 0
	seen := map[uint64]bool{}
	for _, part := range parts {
		if part.PartCount != 4 {
			t.Fatal("wrong part count")
		}
		for _, player := range part.Players {
			count++
			if seen[player.PlayerId] || player.PlayerId == distant.id {
				t.Fatal("duplicate or distant entity")
			}
			seen[player.PlayerId] = true
		}
	}
	if count != 65 || !seen[observer.id] {
		t.Fatalf("incomplete baseline: %d", count)
	}
}
func TestReliableActionDeduplicatesAndSurvivesResume(t *testing.T) {
	now := time.Now()
	s := newServer(netcode.DefaultConfig(), now, 100)
	current := testHello(s, 50000, now)
	data, _ := proto.Marshal(&pb.DiagnosticAction{Echo: "once"})
	message := command(current)
	message.Event = &pb.ClientEvents_Reliable{Reliable: &pb.ReliableFrame{Sequence: 1, Payload: data}}
	for i := 0; i < 3; i++ {
		reply := s.handle(current.address, message, now)
		if reply.GetReliableAck().Sequence != 1 {
			t.Fatal("missing acknowledgement")
		}
	}
	if current.reliable.Pending() != 1 {
		t.Fatal("action executed multiple times")
	}
}
func TestLossReorderAndOutageRecoverWithoutBacklog(t *testing.T) {
	for _, loss := range []float64{0, 0.01, 0.05, 0.1} {
		now := time.Unix(100, 0)
		s := newServer(netcode.DefaultConfig(), now, 100)
		current := testHello(s, 50000, now)
		rng := rand.New(rand.NewSource(42))
		type delivery struct {
			at     int
			packet *pb.ClientEvents
		}
		queue := []delivery{}
		for frame := 1; frame <= 1200; frame++ {
			// 150 ms RTT plus bounded jitter; future inputs repeat in recent batches.
			if frame < 300 || frame > 600 {
				inputs := []*pb.PlayerInput{}
				for tick := max(1, frame+12-15); tick <= frame+12; tick++ {
					inputs = append(inputs, &pb.PlayerInput{Sequence: uint64(tick), MoveX: 1})
				}
				if rng.Float64() >= loss {
					queue = append(queue, delivery{frame + 3 + rng.Intn(4), command(current, inputs...)})
				}
			}
			rng.Shuffle(len(queue), func(i, j int) { queue[i], queue[j] = queue[j], queue[i] })
			remaining := queue[:0]
			at := now.Add(time.Duration(frame) * time.Second / 60)
			for _, packet := range queue {
				if packet.at <= frame {
					s.handle(current.address, packet.packet, at)
				} else {
					remaining = append(remaining, packet)
				}
			}
			queue = remaining
			s.advance(at)
			if current.player.Timeline.Len() > 256 {
				t.Fatal("unbounded input history")
			}
		}
		if current.player.Timeline.Cursor < 1199 || current.player.Timeline.Applied < 1190 || current.player.Timeline.Len() > 20 {
			t.Fatalf("backlog after recovery with loss %.2f", loss)
		}
		if current.player.X < 65 || current.player.X > 77 {
			t.Fatalf("unexpected movement after five-second outage: %f", current.player.X)
		}
	}
}
func TestEnvelopeValidation(t *testing.T) {
	for _, data := range [][]byte{nil, {255}, {8, 2}, make([]byte, 1101)} {
		if _, err := protocol.DecodeClientEvents(data); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	message := &pb.ClientEvents{ProtocolVersion: 1, Event: &pb.ClientEvents_Hello{Hello: &pb.Hello{}}}
	data, _ := proto.Marshal(message)
	if _, err := protocol.DecodeClientEvents(data); err == nil {
		t.Fatal("old protocol accepted")
	}
	message.ProtocolVersion = protocol.Version
	data, _ = proto.Marshal(message)
	if _, err := protocol.DecodeClientEvents(data); err != nil {
		t.Fatal(err)
	}
}
