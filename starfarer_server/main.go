package main

import (
	"flag"
	"log"
	"net"
	"time"

	"starfarer/internal/protocol"
	pb "starfarer/internal/protocol/v1"
	"starfarer/internal/world"
)

type session struct {
	id       uint64
	address  *net.UDPAddr
	lastSeen time.Time
	player   *world.Player
}

type server struct {
	sessions map[string]*session
	nextID   uint64
}

func (s *server) handle(address *net.UDPAddr, message *pb.ClientEvents, now time.Time) *pb.ServerEvents {
	key := address.String()
	current := s.sessions[key]
	if current != nil && now.Sub(current.lastSeen) > 15*time.Second {
		delete(s.sessions, key)
		current = nil
	}
	result := &pb.ServerEvents{ProtocolVersion: protocol.Version}
	switch event := message.Event.(type) {
	case *pb.ClientEvents_Hello:
		if current == nil {
			if len(s.sessions) >= 128 {
				return nil
			}
			s.nextID++
			current = &session{
				id:      s.nextID,
				address: address,
				player:  world.NewPlayer(s.nextID),
			}
			s.sessions[key] = current
			log.Printf("HELLO peer=%s player=%d client=%q", key, current.id, event.Hello.ClientVersion)
		}
		current.lastSeen = now
		result.Event = &pb.ServerEvents_Welcome{Welcome: &pb.Welcome{PlayerId: current.id}}
	case *pb.ClientEvents_PlayerInput:
		if current == nil || event.PlayerInput == nil {
			return nil
		}

		input := event.PlayerInput

		if current.player.ApplyInput(
			input.Sequence,
			input.MoveX,
			input.MoveZ,
			now,
		) {
			current.lastSeen = now
		}

		return nil
	default:
		return nil
	}
	return result
}

// snapshot captures all active players before broadcasting the same state.
func (s *server) snapshot(tick uint64, now time.Time) *pb.WorldSnapshot {
	snapshot := &pb.WorldSnapshot{Tick: tick}
	for key, current := range s.sessions {
		if now.Sub(current.lastSeen) > 15*time.Second {
			log.Printf("EXPIRE player=%d", current.id)
			delete(s.sessions, key)
			continue
		}
		player := current.player
		snapshot.Players = append(snapshot.Players, &pb.PlayerState{
			PlayerId:           player.ID,
			PositionX:          player.X,
			PositionZ:          player.Z,
			LastProcessedInput: player.LastProcessedInput,
		})
	}
	return snapshot
}

func main() {
	listen := flag.String("listen", "127.0.0.1:7777", "local diagnostic UDP address")
	flag.Parse()
	address, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", address)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("LISTEN %s | Protobuf ClientEvents/ServerEvents v%d | local diagnostic, no persistence/auth", conn.LocalAddr(), protocol.Version)
	s := &server{sessions: make(map[string]*session)}
	send := func(address *net.UDPAddr, event *pb.ServerEvents) {
		data, err := protocol.EncodeServerEvents(event)
		if err != nil {
			log.Printf("encode: %v", err)
			return
		}
		if _, err = conn.WriteToUDP(data, address); err != nil {
			log.Printf("send: %v", err)
		}
	}
	buffer := make([]byte, 65535)
	const simulationHz = 30
	const snapshotEvery = 3 // 30 / 3 = 10 snapshots por segundo.
	const maxCatchUpSteps = 5

	stepInterval := time.Second / simulationHz
	const dt float32 = 1.0 / simulationHz

	nextTick := time.Now().Add(stepInterval)
	var tick uint64
	for {
		if err := conn.SetReadDeadline(nextTick); err != nil {
			log.Fatal(err)
		}
		n, peer, err := conn.ReadFromUDP(buffer)
		now := time.Now()
		if err == nil {
			event, decodeErr := protocol.DecodeClientEvents(buffer[:n])
			if decodeErr != nil {
				log.Printf("REJECT peer=%s: %v", peer, decodeErr)
			} else if reply := s.handle(peer, event, now); reply != nil {
				send(peer, reply)
			}
		} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			log.Fatal(err)
		}

		steps := 0

		for !now.Before(nextTick) && steps < maxCatchUpSteps {
			tick++
			steps++

			for _, current := range s.sessions {
				current.player.Step(dt, now)
			}

			if tick%snapshotEvery == 0 {
				event := &pb.ServerEvents{
					ProtocolVersion: protocol.Version,
					Event: &pb.ServerEvents_Snapshot{
						Snapshot: s.snapshot(tick, now),
					},
				}

				for _, current := range s.sessions {
					send(current.address, event)
				}
			}

			if tick%simulationHz == 0 && len(s.sessions) > 0 {
				log.Printf(
					"WORLD tick=%d players=%d",
					tick,
					len(s.sessions),
				)
			}

			nextTick = nextTick.Add(stepInterval)
		}

		// Evita acumular trabajo indefinidamente si el servidor se retrasa.
		if !now.Before(nextTick) {
			nextTick = now.Add(stepInterval)
		}
	}
}
