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

// CabService is the RPC receiver registered on each node.
// Adapted from cabinet/service.go.
type CabService struct {
	nodeID        int
	Priority      *smr.PriorityState
	myState       *smr.ServerState
	KVExecutor    func(op, key, valueJSON string) error
	lastHeartbeat atomic.Int64 // Unix nanoseconds; updated by Heartbeat RPC
	beatCount     atomic.Int64 // counts heartbeats received, for log throttling
}

func NewCabService(nodeID int, p *smr.PriorityState, state *smr.ServerState, exec func(op, key, valueJSON string) error) *CabService {
	svc := &CabService{nodeID: nodeID, Priority: p, myState: state, KVExecutor: exec}
	svc.lastHeartbeat.Store(time.Now().UnixNano())
	return svc
}

// Heartbeat is the RPC method the leader calls periodically.
// It refreshes the follower's lastHeartbeat timestamp and syncs the leaderID.
// Heartbeats from a lower-term sender (stale leader) are silently ignored.
func (s *CabService) Heartbeat(args *HeartbeatArgs, reply *HeartbeatReply) error {
	if args.Term < s.myState.GetTerm() {
		// Stale leader — do not reset the timeout so the current leader is
		// still recognised as legitimate.
		reply.Success = false
		return nil
	}
	s.lastHeartbeat.Store(time.Now().UnixNano())
	s.myState.SetLeaderID(args.LeaderID)
	reply.Success = true
	n := s.beatCount.Add(1)
	if n%10 == 0 {
		fmt.Printf("[Node %d | Follower  | RPC ] heartbeat from leader %d (term %d, beat #%d)\n",
			s.nodeID, args.LeaderID, args.Term, n)
	}
	return nil
}

// RequestVote is the RPC method candidates call during a leader election.
// Implements Raft vote-granting rules: grant if candidate's term > our term.
func (s *CabService) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	myTerm := s.myState.GetTerm()
	reply.Term = myTerm

	if args.Term <= myTerm {
		reply.VoteGranted = false
		fmt.Printf("[Node %d | Follower  | RPC ] denied vote for node %d (term %d <= my term %d)\n",
			s.nodeID, args.CandidateID, args.Term, myTerm)
		return nil
	}

	if s.myState.TryVote(args.Term, args.CandidateID) {
		s.myState.SetTerm(args.Term)
		reply.VoteGranted = true
		fmt.Printf("[Node %d | Follower  | RPC ] voted for node %d (term %d)\n", s.nodeID, args.CandidateID, args.Term)
	} else {
		reply.VoteGranted = false
		fmt.Printf("[Node %d | Follower  | RPC ] already voted this term, denied node %d\n", s.nodeID, args.CandidateID)
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
			elapsed := time.Since(last)
			if elapsed > HeartbeatTimeout {
				fmt.Printf("[Node %d | Follower  | HB  ] ✗ timeout — last beat %dms ago (limit %dms) — leader may be down\n",
					s.nodeID, elapsed.Milliseconds(), HeartbeatTimeout.Milliseconds())
				onTimeout()
				return
			}
			n := s.beatCount.Load()
			if n%10 == 0 {
				fmt.Printf("[Node %d | Follower  | HB  ] ✓ leader alive — last beat %dms ago (limit %dms)\n",
					s.nodeID, elapsed.Milliseconds(), HeartbeatTimeout.Milliseconds())
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
