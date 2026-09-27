package main

import (
	"fmt"
	"math"
	"math/rand"
	pb "starfarer/internal/protocol/v1"
	"starfarer/netcode"
	"testing"
	"time"
)

func TestNetworkConditionMatrix(t *testing.T) {
	for _, rtt := range []int{0, 50, 150, 300} {
		for _, jitter := range []int{0, 20, 60} {
			for _, loss := range []float64{0, .01, .05, .10} {
				for _, outage := range []int{60, 300} {
					t.Run(fmt.Sprintf("rtt%d_jitter%d_loss%.2f_outage%d", rtt, jitter, loss, outage), func(t *testing.T) {
						started := time.Unix(100, 0)
						s := newServer(netcode.DefaultConfig(), started, 100)
						current := testHello(s, 50000, started)
						rng := rand.New(rand.NewSource(84))
						lead := int(math.Ceil((float64(rtt)/2 + float64(jitter) + 50) * 60 / 1000))
						type delivery struct {
							at      int
							message *pb.ClientEvents
						}
						pending := []delivery{}
						for frame := 1; frame <= 1200; frame++ {
							now := started.Add(time.Duration(frame) * time.Second / 60)
							if frame < 240 || frame >= 240+outage {
								inputs := []*pb.PlayerInput{}
								for tick := max(1, frame+lead-15); tick <= frame+lead; tick++ {
									inputs = append(inputs, &pb.PlayerInput{Sequence: uint64(tick), MoveX: 1})
								}
								if rng.Float64() >= loss {
									delay := max(0, (float64(rtt)/2+(rng.Float64()*2-1)*float64(jitter))/1000)
									message := command(current, inputs...)
									at := frame + int(math.Ceil(delay*60))
									pending = append(pending, delivery{at, message})
									if rng.Float64() < .03 {
										pending = append(pending, delivery{at + 2, message})
									}
								}
							}
							rng.Shuffle(len(pending), func(i, j int) { pending[i], pending[j] = pending[j], pending[i] })
							remaining := pending[:0]
							for _, packet := range pending {
								if packet.at <= frame {
									s.handle(current.address, packet.message, now)
								} else {
									remaining = append(remaining, packet)
								}
							}
							pending = remaining
							s.advance(now)
							if current.player.Timeline.Len() > 256 {
								t.Fatal("unbounded history")
							}
						}
						if current.player.Timeline.Cursor < 1199 || current.player.Timeline.Applied < 1188 || current.player.Timeline.Len() > 32 {
							t.Fatal("permanent backlog after recovery")
						}
						expected := float32(1200-outage) * 5 / 60
						if current.player.X > expected+2 || current.player.X < expected-5 {
							t.Fatalf("unbounded/lost movement: %f expected approximately %f", current.player.X, expected)
						}
					})
				}
			}
		}
	}
}
