package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"log"
	"math"
	"net"
	"sort"
	"time"

	"google.golang.org/protobuf/proto"

	"starfarer/internal/protocol"
	pb "starfarer/internal/protocol/v1"
	"starfarer/internal/world"
	"starfarer/netcode"
)

type session struct {
	id, epoch, attempt                           uint64
	token, connection                            []byte
	address                                      *net.UDPAddr
	lastSeen, nextSnapshot, lastAckAt, nextAdapt time.Time
	player                                       *world.Player
	reliable                                     netcode.ReliableChannel
	ingress, egress                              *netcode.Budget
	visible                                      map[uint64]bool
	rate                                         int
	lastSent, ack                                uint64
}
type server struct {
	sessions               map[string]*session
	nextID, tick, revision uint64
	config                 netcode.Config
	started                time.Time
	radius                 float64
	index                  *netcode.SpatialIndex
	ingress                *netcode.Budget
}

func newServer(config netcode.Config, now time.Time, radius float64) *server {
	return &server{sessions: make(map[string]*session), config: config, started: now, revision: 1, radius: radius, index: netcode.NewSpatialIndex(radius), ingress: netcode.NewBudget(8000, 16000, now)}
}
func (s *server) seconds(now time.Time) float64 { return max(0, now.Sub(s.started).Seconds()) }
func (s *server) state(current *session) *pb.PlayerState {
	return &pb.PlayerState{PlayerId: current.id, PositionX: current.player.X, PositionZ: current.player.Z, LastProcessedInput: current.player.Timeline.Applied, ResolvedTick: current.player.Timeline.Cursor}
}
func (s *server) event(current *session) *pb.ServerEvents {
	return &pb.ServerEvents{ProtocolVersion: protocol.Version, SessionEpoch: current.epoch}
}
func (s *server) handle(address *net.UDPAddr, message *pb.ClientEvents, now time.Time) *pb.ServerEvents {
	if !s.ingress.Allow(1, now) {
		return nil
	}
	if hello := message.GetHello(); hello != nil {
		if len(hello.ConnectionId) != 16 || hello.Attempt == 0 || len(hello.ClientVersion) > 64 || (len(hello.ResumeToken) != 0 && len(hello.ResumeToken) != 32) || math.IsNaN(hello.ClientTime) || math.IsInf(hello.ClientTime, 0) {
			return nil
		}
		current := s.sessions[string(hello.ResumeToken)]
		if current == nil {
			// Repeat an initial Hello without creating another player.
			for _, candidate := range s.sessions {
				if bytes.Equal(candidate.connection, hello.ConnectionId) && candidate.address.String() == address.String() {
					current = candidate
					break
				}
			}
		}
		if current == nil {
			if len(s.sessions) >= 128 {
				return nil
			}
			s.nextID++
			token := make([]byte, 32)
			if _, err := rand.Read(token); err != nil {
				return nil
			}
			var epochBytes [8]byte
			if _, err := rand.Read(epochBytes[:]); err != nil {
				return nil
			}
			epoch := binary.BigEndian.Uint64(epochBytes[:])
			if epoch == 0 {
				epoch = 1
			}
			current = &session{
				id: s.nextID, epoch: epoch, attempt: hello.Attempt, token: token,
				connection: append([]byte(nil), hello.ConnectionId...), address: address,
				player: world.NewPlayerAt(s.nextID, s.tick), rate: s.config.SnapshotHz,
				visible: make(map[uint64]bool), lastAckAt: now,
				ingress: netcode.NewBudget(240, 480, now), egress: netcode.NewBudget(32768, 16384, now),
			}
			s.sessions[string(token)] = current
			log.Printf("JOIN player=%d peer=%s", current.id, address)
		} else if !bytes.Equal(current.connection, hello.ConnectionId) {
			if hello.Attempt <= current.attempt || current.epoch == ^uint64(0) {
				return nil
			}
			current.epoch++
			current.attempt = hello.Attempt
			current.connection = append([]byte(nil), hello.ConnectionId...)
			current.address = address
			current.player.Timeline.ClearPending()
			current.lastSent = 0
			current.ack = 0
			current.lastAckAt = now
			log.Printf("RESUME player=%d epoch=%d peer=%s", current.id, current.epoch, address)
		} else if current.address.String() != address.String() {
			return nil
		}
		if !current.ingress.Allow(1, now) {
			return nil
		}
		current.lastSeen = now
		reply := s.event(current)
		reply.Event = &pb.ServerEvents_Welcome{Welcome: &pb.Welcome{
			PlayerId: current.id, SimulationHz: uint32(s.config.SimulationHz), SnapshotHz: uint32(current.rate),
			InputBatchLimit: 16, InputHistoryLimit: 256, InputHz: world.InputHz,
			ConnectionId: current.connection, ResumeToken: current.token, SessionEpoch: current.epoch,
			ServerTime: s.seconds(now), Player: s.state(current), ActionAck: current.reliable.Received, ClientTime: hello.ClientTime,
		}}
		return reply
	}
	current := s.sessions[string(message.ResumeToken)]
	if current == nil || message.SessionEpoch != current.epoch || address.String() != current.address.String() || !current.ingress.Allow(1, now) {
		return nil
	}
	current.lastSeen = now
	if message.SnapshotAck > current.ack && message.SnapshotAck <= current.lastSent {
		current.ack = message.SnapshotAck
		current.lastAckAt = now
	}
	reply := s.event(current)
	switch event := message.Event.(type) {
	case *pb.ClientEvents_PlayerInputs:
		if event.PlayerInputs == nil || len(event.PlayerInputs.Inputs) > 16 {
			return nil
		}
		for _, input := range event.PlayerInputs.Inputs {
			if input != nil {
				current.player.ApplyInput(input.Sequence, input.MoveX, input.MoveZ)
			}
		}
	case *pb.ClientEvents_ClockProbe:
		if event.ClockProbe == nil || math.IsNaN(event.ClockProbe.ClientTime) || math.IsInf(event.ClockProbe.ClientTime, 0) {
			return nil
		}
		reply.Event = &pb.ServerEvents_ClockReply{ClockReply: &pb.ClockReply{ClientTime: event.ClockProbe.ClientTime, ServerTime: s.seconds(now)}}
		return reply
	case *pb.ClientEvents_Reliable:
		if event.Reliable == nil {
			return nil
		}
		ack := current.reliable.Receive(event.Reliable.Sequence, event.Reliable.Payload, func(payload []byte) bool {
			action := new(pb.DiagnosticAction)
			if proto.Unmarshal(payload, action) != nil || len(action.Echo) > 256 {
				return false
			}
			result, _ := proto.Marshal(&pb.DiagnosticResult{Echo: action.Echo})
			return current.reliable.Enqueue(result)
		})
		reply.Event = &pb.ServerEvents_ReliableAck{ReliableAck: &pb.ReliableAck{Sequence: ack}}
		return reply
	case *pb.ClientEvents_ReliableAck:
		if event.ReliableAck != nil {
			current.reliable.Ack(event.ReliableAck.Sequence)
		}
	}
	return nil
}
func (s *server) advance(now time.Time) {
	s.tick = uint64(s.seconds(now) * world.InputHz)
	for key, current := range s.sessions {
		if now.Sub(current.lastSeen) > 2*time.Minute {
			log.Printf("EXPIRE player=%d", current.id)
			delete(s.sessions, key)
			continue
		}
		current.player.Advance(s.tick)
	}
}
func (s *server) service(now time.Time, send func(*net.UDPAddr, *pb.ServerEvents)) {
	for _, current := range s.sessions {
		if now.Sub(current.lastSeen) > 5*time.Second {
			continue
		}
		if frame, ok := current.reliable.Due(now, 250*time.Millisecond); ok {
			reply := s.event(current)
			reply.Event = &pb.ServerEvents_Reliable{Reliable: &pb.ReliableFrame{Sequence: frame.Sequence, Payload: frame.Payload}}
			if current.egress.Allow(float64(proto.Size(reply)), now) {
				send(current.address, reply)
			}
		}
	}
}
func (s *server) snapshots(now time.Time, send func(*net.UDPAddr, *pb.ServerEvents)) {
	points := []netcode.Point{}
	byID := map[uint64]*session{}
	for _, current := range s.sessions {
		if now.Sub(current.lastSeen) <= 5*time.Second {
			points = append(points, netcode.Point{ID: current.id, X: float64(current.player.X), Z: float64(current.player.Z)})
			byID[current.id] = current
		}
	}
	s.index.Rebuild(points)
	for _, current := range byID {
		if !now.Before(current.nextAdapt) {
			if now.Sub(current.lastAckAt) > time.Second {
				current.rate = max(2, current.rate/2)
			} else if current.rate < s.config.SnapshotHz {
				current.rate++
			}
			current.rate = min(current.rate, s.config.SnapshotHz)
			current.nextAdapt = now.Add(time.Second)
		}
		if now.Before(current.nextSnapshot) {
			continue
		}
		current.nextSnapshot = now.Add(time.Second / time.Duration(current.rate))
		ids := s.index.Select(netcode.Point{ID: current.id, X: float64(current.player.X), Z: float64(current.player.Z)}, s.radius, s.radius*1.2, 128, current.visible)
		// Complete baselines repair lost spawn/despawn without a separate lifecycle channel.
		nextVisible := make(map[uint64]bool, len(ids))
		for _, id := range ids {
			nextVisible[id] = true
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		parts := (len(ids) + 19) / 20
		events := make([]*pb.ServerEvents, 0, parts)
		cost := 0
		for part := 0; part < parts; part++ {
			snapshot := &pb.WorldSnapshot{Tick: s.tick, PartIndex: uint32(part), PartCount: uint32(parts), ServerTime: float64(s.tick) / world.InputHz, SnapshotHz: uint32(current.rate), TimingRevision: s.revision, SimulationHz: uint32(s.config.SimulationHz)}
			for _, id := range ids[part*20 : min(len(ids), (part+1)*20)] {
				snapshot.Players = append(snapshot.Players, s.state(byID[id]))
			}
			event := s.event(current)
			event.Event = &pb.ServerEvents_Snapshot{Snapshot: snapshot}
			cost += proto.Size(event) + 52
			events = append(events, event)
		}
		if current.egress.Allow(float64(cost), now) {
			for _, event := range events {
				send(current.address, event)
			}
			current.lastSent = s.tick
			current.visible = nextVisible
		} else {
			current.rate = max(1, current.rate/2)
			current.nextSnapshot = now.Add(time.Second / time.Duration(current.rate))
		}
	}
}
