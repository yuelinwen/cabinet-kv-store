package cabinet

import (
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
	Priority   *smr.PriorityState
	// KVExecutor is injected by the node setup and applies the command to
	// this node's own MongoDB collection.
	KVExecutor func(op, key, valueJSON string) error
}

func NewCabService(p *smr.PriorityState, exec func(op, key, valueJSON string) error) *CabService {
	return &CabService{Priority: p, KVExecutor: exec}
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
