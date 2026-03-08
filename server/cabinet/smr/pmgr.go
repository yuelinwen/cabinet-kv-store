package smr

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

type serverID = int
type prioClock = int
type priority = float64

type PriorityManager struct {
	sync.RWMutex
	m        map[prioClock]map[serverID]priority
	scheme   []priority
	majority float64
	n        int
	q        int
}

func (pm *PriorityManager) Init(numOfServers, quorumSize, baseOfPriorities int, ratioTryStep float64, isCab bool) {
	pm.n = numOfServers
	pm.q = quorumSize // quorum size is t+1
	pm.m = make(map[prioClock]map[serverID]priority)

	ratio := 1.0
	if isCab {
		ratio = calcInitPrioRatio(numOfServers, quorumSize, ratioTryStep)
	}
	fmt.Println("[Cabinet] priority ratio:", ratio)

	newPriorities := make(map[serverID]priority)

	for i := 0; i < numOfServers; i++ {
		p := float64(baseOfPriorities) * math.Pow(ratio, float64(i))
		newPriorities[numOfServers-1-i] = p
		pm.scheme = append(pm.scheme, p)
	}

	reverseSlice(pm.scheme)

	pm.majority = sum(pm.scheme) / 2

	pm.Lock()
	pm.m[0] = newPriorities
	pm.Unlock()
}

func reverseSlice(slice []priority) {
	length := len(slice)
	for i := 0; i < length/2; i++ {
		j := length - 1 - i
		slice[i], slice[j] = slice[j], slice[i]
	}
}

// calcInitPrioRatio finds ratio r (geometric sequence base) satisfying Cabinet's
// two invariants:
//   I1: sum(top t+1 weights) > majority  (quorum can always be formed)
//   I2: sum(top t weights)   < majority  (no subset smaller than quorum suffices)
func calcInitPrioRatio(n, f int, ratioTryStep float64) (ratio float64) {
	r := 2.0
	for {
		if math.Pow(r, float64(n-f+1)) > 0.5*(math.Pow(r, float64(n))+1) &&
			0.5*(math.Pow(r, float64(n))+1) > math.Pow(r, float64(n-f)) {
			return r
		}
		r -= ratioTryStep
	}
}

func sum(arr []float64) float64 {
	total := 0.0
	for _, val := range arr {
		total += val
	}
	return total
}

// UpdateFollowerPriorities re-ranks follower weights for pClock based on the
// order nodes replied in the previous round (prioQueue, fastest first).
// Fastest responder → scheme[1], second fastest → scheme[2], etc.
// Non-responding followers get the remaining (lowest) weights.
func (pm *PriorityManager) UpdateFollowerPriorities(pClock prioClock, prioQueue chan serverID, leaderID serverID) error {
	newPriorities := make(map[serverID]priority)
	arranged := make(map[serverID]bool)

	for i := 0; i < pm.n; i++ {
		arranged[i] = false
	}

	nr := len(prioQueue)

	for i := 0; i < nr; i++ {
		s := <-prioQueue
		newPriorities[s] = pm.scheme[i+1] // fastest gets scheme[1], next scheme[2]…
		arranged[s] = true
	}

	i := nr + 1

	for id, done := range arranged {
		if !done {
			if id == leaderID {
				newPriorities[id] = pm.scheme[0] // leader always keeps highest weight
				continue
			}
			if i == len(pm.scheme) {
				err := fmt.Sprintf("priority assignment of [%v] exceeds pm scheme length [%v]", i, len(pm.scheme))
				return errors.New(err)
			}
			newPriorities[id] = pm.scheme[i]
			i++
		}
	}

	pm.Lock()
	pm.m[pClock] = newPriorities
	pm.Unlock()
	return nil
}

func (pm *PriorityManager) GetFollowerPriorities(pClock int) (fpriorities map[serverID]priority) {
	fpriorities = make(map[serverID]priority)
	pm.RLock()
	defer pm.RUnlock()
	fpriorities = pm.m[pClock]
	return
}

func (pm *PriorityManager) GetMajority() float64      { return pm.majority }
func (pm *PriorityManager) GetPriorityScheme() []priority { return pm.scheme }
func (pm *PriorityManager) GetQuorumSize() int         { return pm.q }
