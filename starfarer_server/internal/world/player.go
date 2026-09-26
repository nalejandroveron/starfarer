package world

import (
	"math"
	"time"
)

type Player struct {
	ID uint64

	// Posición autoritativa calculada por Go.
	X float32
	Z float32

	// Última dirección recibida del cliente.
	MoveX float32
	MoveZ float32

	LastReceivedInput  uint64
	LastProcessedInput uint64
	LastInputAt        time.Time
}

func NewPlayer(id uint64) *Player {
	return &Player{
		ID: id,
	}
}

func (p *Player) ApplyInput(
	sequence uint64,
	moveX, moveZ float32,
	now time.Time,
) bool {
	if sequence <= p.LastReceivedInput {
		return false
	}

	x, z := float64(moveX), float64(moveZ)

	if math.IsNaN(x) || math.IsNaN(z) ||
		math.IsInf(x, 0) || math.IsInf(z, 0) {
		return false
	}

	// Limita la magnitud a 1, conservando la dirección.
	length := math.Hypot(x, z)
	if length > 1 {
		x /= length
		z /= length
	}

	p.MoveX = float32(x)
	p.MoveZ = float32(z)
	p.LastReceivedInput = sequence
	p.LastInputAt = now

	return true
}

const (
	playerSpeed  float32 = 5 // Unidades por segundo.
	inputTimeout         = 250 * time.Millisecond
)

func (p *Player) Step(dt float32, now time.Time) {
	if p.LastInputAt.IsZero() ||
		now.Sub(p.LastInputAt) > inputTimeout {
		p.MoveX = 0
		p.MoveZ = 0
		return
	}

	p.X += p.MoveX * playerSpeed * dt
	p.Z += p.MoveZ * playerSpeed * dt

	p.LastProcessedInput = p.LastReceivedInput
}
