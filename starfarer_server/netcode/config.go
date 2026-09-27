// Package netcode contains transport and timing primitives independent of game rules.
// All stateful components are owned by one loop unless documented otherwise.
package netcode

import (
	"fmt"
	"time"
)

// Leaves room for the optional 52-byte authenticated transport header.
const MaxDatagramSize = 1100

type Config struct {
	SimulationHz     int
	SnapshotHz       int
	MaxCatchUpSteps  int
	MaxDatagramBytes int
}

func DefaultConfig() Config {
	return Config{SimulationHz: 30, SnapshotHz: 10, MaxCatchUpSteps: 5, MaxDatagramBytes: MaxDatagramSize}
}

func (c Config) Validate() error {
	if c.SimulationHz < 1 || c.SimulationHz > 240 || c.SnapshotHz < 1 || c.SnapshotHz > 240 {
		return fmt.Errorf("simulation and snapshot rates must be between 1 and 240 Hz")
	}
	if c.MaxCatchUpSteps < 1 || c.MaxCatchUpSteps > 100 || c.MaxDatagramBytes < 1 || c.MaxDatagramBytes > 65507 {
		return fmt.Errorf("invalid catch-up or datagram limit")
	}
	return nil
}

func (c Config) StepDuration() time.Duration { return time.Second / time.Duration(c.SimulationHz) }
func (c Config) StepSeconds() float32        { return 1 / float32(c.SimulationHz) }
