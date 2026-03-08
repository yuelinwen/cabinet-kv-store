package smr

import (
	"errors"
	"sync"
)

// PriorityState tracks this node's current Cabinet priority weight.
// On the leader it holds the leader's own weight; on followers it is
// updated every round via the ConsensusService RPC.
type PriorityState struct {
	sync.RWMutex
	PrioClock int
	PrioVal   float64
	Majority  float64
}

func NewServerPriority(initPrioClock int, initPrioVal float64) PriorityState {
	return PriorityState{
		PrioClock: initPrioClock,
		PrioVal:   initPrioVal,
	}
}

// UpdatePriority advances this node's priority to the new pClock value.
// Rejects updates with a lower pClock (monotonically non-decreasing).
func (p *PriorityState) UpdatePriority(newPClock int, newPriority float64) error {
	p.Lock()
	defer p.Unlock()
	if newPClock < p.PrioClock {
		return errors.New("newPClock is less than current PClock")
	}
	p.PrioClock = newPClock
	p.PrioVal = newPriority
	return nil
}

func (p *PriorityState) GetPriority() (pClock int, pValue float64) {
	p.RLock()
	defer p.RUnlock()
	return p.PrioClock, p.PrioVal
}
