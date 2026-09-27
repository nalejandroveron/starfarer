package netcode

import "time"

// Scheduler separates simulation time from snapshot cadence.
// Excess backlog is observable; the caller decides its overload policy.
type Scheduler struct {
	config                 Config
	nextStep, nextSnapshot time.Time
	Tick                   uint64
	DroppedSteps           uint64
}

type Advance struct {
	Steps    int
	Snapshot bool
}

func NewScheduler(config Config, now time.Time) (*Scheduler, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Scheduler{config: config, nextStep: now.Add(config.StepDuration()), nextSnapshot: now.Add(time.Second / time.Duration(config.SnapshotHz))}, nil
}

func (s *Scheduler) Deadline() time.Time {
	if s.nextSnapshot.Before(s.nextStep) {
		return s.nextSnapshot
	}
	return s.nextStep
}

// Input time is independent of these callback/snapshot delivery frequencies.
func (s *Scheduler) Configure(config Config, now time.Time) error {
	if err := config.Validate(); err != nil {
		return err
	}
	s.config = config
	s.nextStep = now.Add(config.StepDuration())
	s.nextSnapshot = now.Add(time.Second / time.Duration(config.SnapshotHz))
	return nil
}

func (s *Scheduler) Advance(now time.Time) Advance {
	var result Advance
	step := s.config.StepDuration()
	for !now.Before(s.nextStep) && result.Steps < s.config.MaxCatchUpSteps {
		result.Steps++
		s.Tick++
		s.nextStep = s.nextStep.Add(step)
	}
	if !now.Before(s.nextStep) {
		s.DroppedSteps += uint64(now.Sub(s.nextStep)/step) + 1
		s.nextStep = now.Add(step)
	}
	if !now.Before(s.nextSnapshot) {
		result.Snapshot = true
		// Never burst old snapshots after a stall.
		s.nextSnapshot = now.Add(time.Second / time.Duration(s.config.SnapshotHz))
	}
	return result
}
