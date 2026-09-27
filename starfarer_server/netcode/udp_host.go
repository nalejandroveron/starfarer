package netcode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// HostCallbacks execute serially. Data passed to Receive is only valid during
// that callback. Game decoders, session identity and world state stay outside.
type HostCallbacks struct {
	Receive   func(*net.UDPAddr, []byte, time.Time)
	Step      func(float32, time.Time)
	Snapshot  func(uint64, time.Time)
	Report    func(error)
	Configure func(time.Time) *Config
}

type UDPHost struct {
	conn      *net.UDPConn
	config    Config
	protector *Protector
	senders   map[string]*protectedSender
}
type protectedSender struct {
	protector *Protector
	used      time.Time
}

func ListenUDP(address string, config Config) (*UDPHost, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	return &UDPHost{conn: conn, config: config}, nil
}

func (h *UDPHost) Address() net.Addr { return h.conn.LocalAddr() }
func (h *UDPHost) Close() error      { return h.conn.Close() }
func (h *UDPHost) SetProtection(p *Protector) {
	h.protector = p
	h.senders = make(map[string]*protectedSender)
}
func (h *UDPHost) Send(peer *net.UDPAddr, data []byte) error {
	if len(data) == 0 || len(data) > h.config.MaxDatagramBytes {
		return fmt.Errorf("invalid datagram size: %d", len(data))
	}
	if h.protector != nil {
		var err error
		now := time.Now()
		sender := h.senders[peer.String()]
		if sender == nil {
			for key, value := range h.senders {
				if now.Sub(value.used) > 2*time.Minute {
					delete(h.senders, key)
				}
			}
			if len(h.senders) >= 256 {
				return fmt.Errorf("protected sender capacity reached")
			}
			protector, createErr := NewProtector(h.protector.secret)
			if createErr != nil {
				return createErr
			}
			sender = &protectedSender{protector: protector}
			h.senders[peer.String()] = sender
		}
		sender.used = now
		data, err = sender.protector.Seal(data)
		if err != nil {
			return err
		}
	}
	_, err := h.conn.WriteToUDP(data, peer)
	return err
}

func (h *UDPHost) Run(ctx context.Context, callbacks HostCallbacks) error {
	scheduler, err := NewScheduler(h.config, time.Now())
	if err != nil {
		return err
	}
	buffer := make([]byte, 65535)
	report := func(err error) {
		if callbacks.Report != nil {
			callbacks.Report(err)
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if callbacks.Configure != nil {
			if updated := callbacks.Configure(time.Now()); updated != nil {
				if err := scheduler.Configure(*updated, time.Now()); err != nil {
					return err
				}
				h.config = *updated
			}
		}
		deadline := scheduler.Deadline()
		if poll := time.Now().Add(250 * time.Millisecond); poll.Before(deadline) {
			deadline = poll
		}
		if err := h.conn.SetReadDeadline(deadline); err != nil {
			return err
		}
		n, peer, err := h.conn.ReadFromUDP(buffer)
		now := time.Now()
		if err == nil {
			packet := buffer[:n]
			if h.protector != nil {
				packet, err = h.protector.Open(packet, now)
				if err != nil {
					packet = nil
				}
			}
			if packet == nil {
				// Authentication failures are silent to avoid log amplification.
			} else if len(packet) == 0 || len(packet) > h.config.MaxDatagramBytes {
				report(fmt.Errorf("discarded datagram with %d bytes", n))
			} else if callbacks.Receive != nil {
				callbacks.Receive(peer, packet, now)
			}
		} else {
			var timeout net.Error
			if !errors.As(err, &timeout) || !timeout.Timeout() {
				return err
			}
		}
		before := scheduler.DroppedSteps
		due := scheduler.Advance(now)
		for i := 0; i < due.Steps; i++ {
			if callbacks.Step != nil {
				callbacks.Step(h.config.StepSeconds(), now)
			}
		}
		if due.Snapshot && callbacks.Snapshot != nil {
			callbacks.Snapshot(scheduler.Tick, now)
		}
		if scheduler.DroppedSteps != before {
			report(fmt.Errorf("simulation overload: dropped %d steps", scheduler.DroppedSteps-before))
		}
	}
}
