package main

import (
 "flag"
 "log"
 "math"
 "net"
 "time"

 "starfarer/internal/protocol"
 pb "starfarer/internal/protocol/v1"
)

type session struct {
 id uint64
 address *net.UDPAddr
 lastSeen time.Time
}

type server struct {
 sessions map[string]*session
 nextID uint64
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
   if len(s.sessions) >= 128 { return nil }
   s.nextID++
   current = &session{id: s.nextID, address: address}
   s.sessions[key] = current
   log.Printf("HELLO peer=%s player=%d client=%q", key, current.id, event.Hello.ClientVersion)
  }
  current.lastSeen = now
  result.Event = &pb.ServerEvents_Welcome{Welcome: &pb.Welcome{PlayerId: current.id}}
 case *pb.ClientEvents_Ping:
  if current == nil { return nil }
  current.lastSeen = now
  log.Printf("PING/PONG player=%d nonce=%d", current.id, event.Ping.Nonce)
  result.Event = &pb.ServerEvents_Pong{Pong: &pb.Pong{Nonce: event.Ping.Nonce}}
 default:
  return nil
 }
 return result
}

func main() {
 listen := flag.String("listen", "127.0.0.1:7777", "local diagnostic UDP address")
 flag.Parse()
 address, err := net.ResolveUDPAddr("udp", *listen)
 if err != nil { log.Fatal(err) }
 conn, err := net.ListenUDP("udp", address)
 if err != nil { log.Fatal(err) }
 defer conn.Close()
 log.Printf("LISTEN %s | Protobuf ClientEvents/ServerEvents v%d | local diagnostic, no persistence/auth", conn.LocalAddr(), protocol.Version)
 s := &server{sessions: make(map[string]*session)}
 send := func(address *net.UDPAddr, event *pb.ServerEvents) {
  data, err := protocol.EncodeServerEvents(event)
  if err != nil { log.Printf("encode: %v", err); return }
  if _, err = conn.WriteToUDP(data, address); err != nil { log.Printf("send: %v", err) }
 }
 buffer := make([]byte, 65535)
 nextTick := time.Now().Add(100*time.Millisecond)
 var tick uint64
 for {
  if err := conn.SetReadDeadline(nextTick); err != nil { log.Fatal(err) }
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
  if !now.Before(nextTick) {
   tick++
   nextTick = now.Add(100*time.Millisecond)
   for key, current := range s.sessions {
    if now.Sub(current.lastSeen) > 15*time.Second {
     log.Printf("EXPIRE player=%d", current.id)
     delete(s.sessions, key)
     continue
    }
    send(current.address, &pb.ServerEvents{ProtocolVersion: protocol.Version, Event: &pb.ServerEvents_Snapshot{Snapshot: &pb.WorldSnapshot{Tick: tick, TestPosition: float32((math.Sin(float64(tick)/15)+1)/2)}}})
   }
   if tick%50 == 0 && len(s.sessions) > 0 { log.Printf("SNAPSHOTS tick=%d clients=%d", tick, len(s.sessions)) }
  }
 }
}
