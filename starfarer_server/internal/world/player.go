package world

import (
	"math"
	"starfarer/netcode"
)

const InputHz = 60
const PlayerSpeed float32 = 5

type movementInput struct{ X, Z float32 }
type Player struct {
	ID       uint64
	X, Z     float32
	Timeline *netcode.Timeline[movementInput]
}

func NewPlayer(id uint64) *Player { return NewPlayerAt(id, 0) }
func NewPlayerAt(id, tick uint64) *Player {
	return &Player{ID: id, Timeline: netcode.NewTimeline[movementInput](tick, 256, 6)}
}
func (p *Player) ApplyInput(tick uint64, x, z float32) bool {
	dx, dz := float64(x), float64(z)
	if math.IsNaN(dx) || math.IsNaN(dz) || math.IsInf(dx, 0) || math.IsInf(dz, 0) {
		return false
	}
	length := math.Hypot(dx, dz)
	if length > 1 {
		x = float32(dx / length)
		z = float32(dz / length)
	}
	return p.Timeline.Add(tick, movementInput{x, z})
}
func (p *Player) Advance(tick uint64) {
	p.Timeline.Advance(tick, 120, func(input movementInput) {
		p.X += input.X * PlayerSpeed / InputHz
		p.Z += input.Z * PlayerSpeed / InputHz
	})
}
