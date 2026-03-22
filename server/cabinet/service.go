package cabinet

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/cabinet/smr"
)

// Args is the payload the leader sends to each follower in every consensus round.
// Adapted from cabinet/service.go — TPCC/PlainMsg/Python fields removed;
// replaced with a simple KV command (Op + Key + ValueJSON).
type Args struct {
	PrioClock PrioClock
	PrioVal   Priority
	Op        string // "PUT" or "DELETE"
	Key       string
	ValueJSON string
}

// Reply is the follower's acknowledgement for one consensus round.
type Reply struct {
	Success bool
	ErrMsg  string
}

// CabService is the RPC receiver registered on each follower node.
// Adapted from cabinet/service.go.
type CabService struct {
	nodeID        int
	Priority      *smr.PriorityState
	KVExecutor    func(op, key, valueJSON string) error
	lastHeartbeat atomic.Int64 // Unix nanoseconds; updated by Heartbeat RPC
	beatCount     atomic.Int64 // counts heartbeats received, for log throttling
}

func NewCabService(nodeID int, p *smr.PriorityState, exec func(op, key, valueJSON string) error) *CabService {
	svc := &CabService{nodeID: nodeID, Priority: p, KVExecutor: exec}
	svc.lastHeartbeat.Store(time.Now().UnixNano())
	return svc
}

// Heartbeat is the RPC method the leader calls periodically.
// It refreshes the follower's lastHeartbeat timestamp.
func (s *CabService) Heartbeat(args *HeartbeatArgs, reply *HeartbeatReply) error {
	s.lastHeartbeat.Store(time.Now().UnixNano())
	reply.Success = true
	n := s.beatCount.Add(1)
	if n%10 == 0 {
		fmt.Printf("[Node %d] heartbeat received from leader (beat #%d)\n", s.nodeID, n)
	}
	return nil
}

// StartHeartbeatMonitor starts a background goroutine that calls onTimeout
// if no heartbeat is received within HeartbeatTimeout.
// onTimeout is called once; the monitor stops after triggering.
func (s *CabService) StartHeartbeatMonitor(onTimeout func()) {
	go func() {
		ticker := time.NewTicker(HeartbeatTimeout / 2)
		defer ticker.Stop()
		for range ticker.C {
			last := time.Unix(0, s.lastHeartbeat.Load())
			if time.Since(last) > HeartbeatTimeout {
				fmt.Printf("[Heartbeat] timeout — leader may be down\n")
				onTimeout()
				return
			}
		}
	}()
}

// ConsensusService is the single RPC method followers expose.
// Step 1: update this node's priority weight for this pClock.
// Step 2: execute the KV command on this node's local MongoDB.
func (s *CabService) ConsensusService(args *Args, reply *Reply) error {
	if err := s.Priority.UpdatePriority(args.PrioClock, args.PrioVal); err != nil {
		reply.ErrMsg = err.Error()
		return err
	}
	if err := s.KVExecutor(args.Op, args.Key, args.ValueJSON); err != nil {
		reply.ErrMsg = err.Error()
		reply.Success = false
		return err
	}
	reply.Success = true
	return nil
}
