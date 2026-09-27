package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"starfarer/internal/protocol"
	pb "starfarer/internal/protocol/v1"
	"starfarer/netcode"
	"time"
)

func main() {
	config := netcode.DefaultConfig()
	listen := flag.String("listen", "127.0.0.1:7777", "UDP listen address")
	settings := flag.String("settings", "", "optional JSON file for live simulation_hz and snapshot_hz updates")
	radius := flag.Float64("aoi-radius", 100, "visibility entry radius in world units")
	flag.IntVar(&config.SimulationHz, "simulation-hz", config.SimulationHz, "server callback frequency; input timeline remains 60 Hz")
	flag.IntVar(&config.SnapshotHz, "snapshot-hz", config.SnapshotHz, "maximum snapshots per client per second")
	flag.Parse()
	if *radius <= 0 || *radius > 100000 || math.IsNaN(*radius) || math.IsInf(*radius, 0) {
		log.Fatal("aoi-radius must be between 0 and 100000")
	}
	host, err := netcode.ListenUDP(*listen, config)
	if err != nil {
		log.Fatal(err)
	}
	defer host.Close()
	secret := os.Getenv("STARFARER_NET_KEY")
	if secret != "" {
		key, err := base64.StdEncoding.DecodeString(secret)
		if err != nil {
			log.Fatal("STARFARER_NET_KEY must be a base64-encoded 32-byte key")
		}
		protector, err := netcode.NewProtector(key)
		if err != nil {
			log.Fatal(err)
		}
		host.SetProtection(protector)
	} else if !host.Address().(*net.UDPAddr).IP.IsLoopback() {
		log.Fatal("non-loopback listeners require STARFARER_NET_KEY")
	}
	log.Printf("LISTEN %s | Protobuf v%d | simulation=%d Hz snapshots=%d Hz | protected=%t", host.Address(), protocol.Version, config.SimulationHz, config.SnapshotHz, secret != "")
	s := newServer(config, time.Now(), *radius)
	send := func(peer *net.UDPAddr, event *pb.ServerEvents) {
		data, err := protocol.EncodeServerEvents(event)
		if err == nil {
			err = host.Send(peer, data)
		}
		if err != nil {
			log.Printf("send: %v", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var nextSettings time.Time
	lastSettings := ""
	err = host.Run(ctx, netcode.HostCallbacks{
		Receive: func(peer *net.UDPAddr, data []byte, now time.Time) {
			event, err := protocol.DecodeClientEvents(data)
			if err != nil {
				return
			}
			if reply := s.handle(peer, event, now); reply != nil {
				send(peer, reply)
			}
		},
		Step:     func(_ float32, now time.Time) { s.advance(now); s.service(now, send) },
		Snapshot: func(_ uint64, now time.Time) { s.snapshots(now, send) },
		Report:   func(err error) { log.Printf("NETCODE %v", err) },
		Configure: func(now time.Time) *netcode.Config {
			if *settings == "" || now.Before(nextSettings) {
				return nil
			}
			nextSettings = now.Add(time.Second)
			data, err := os.ReadFile(*settings)
			if err != nil || len(data) > 4096 || string(data) == lastSettings {
				return nil
			}
			lastSettings = string(data)
			var rates struct {
				SimulationHz int `json:"simulation_hz"`
				SnapshotHz   int `json:"snapshot_hz"`
			}
			if err = json.Unmarshal(data, &rates); err != nil {
				log.Printf("settings: %v", err)
				return nil
			}
			updated := s.config
			updated.SimulationHz = rates.SimulationHz
			updated.SnapshotHz = rates.SnapshotHz
			if err = updated.Validate(); err != nil {
				log.Printf("settings: %v", err)
				return nil
			}
			s.config = updated
			s.revision++
			for _, current := range s.sessions {
				current.rate = updated.SnapshotHz
				current.nextSnapshot = now
			}
			log.Printf("TIMING revision=%d simulation=%d snapshots=%d", s.revision, updated.SimulationHz, updated.SnapshotHz)
			return &updated
		},
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
