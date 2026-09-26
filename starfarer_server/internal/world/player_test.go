package world

import (
	"math"
	"testing"
	"time"
)

func TestPlayerMovementAndTimeout(t *testing.T) {
	player := NewPlayer(1)
	now := time.Now()

	if !player.ApplyInput(1, 1, 0, now) {
		t.Fatal("el input fue rechazado")
	}

	player.Step(1.0/30.0, now)

	expected := 5.0 / 30.0
	if math.Abs(float64(player.X)-expected) > 0.00001 {
		t.Fatalf("posición inesperada: %f", player.X)
	}

	if player.LastProcessedInput != 1 {
		t.Fatal("no se registró el input procesado")
	}

	previousX := player.X
	player.Step(1.0/30.0, now.Add(300*time.Millisecond))

	if player.X != previousX {
		t.Fatal("el jugador siguió moviéndose sin inputs recientes")
	}
}
