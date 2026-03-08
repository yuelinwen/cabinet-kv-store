package cabinet

import (
	"fmt"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/cabinet/smr"
)

const serviceMethod = "CabService.ConsensusService"

// RunConsensus is the leader's Cabinet consensus loop.
// Adapted from cabinet/consensus.go — TPCC/PlainMsg/eval/crash code removed.
//
// Each iteration of the outer for-loop is one Cabinet round (one pClock):
//  1. Get each follower's weight for this round from pManager.
//  2. Broadcast ConsensusService RPC to all followers concurrently.
//  3. Collect replies in arrival order into prioQueue (fastest first).
//  4. Accumulate weights; stop when prioSum > majority (weighted quorum).
//  5. Commit the command to the leader's own MongoDB.
//  6. Re-rank follower weights based on reply order (prioQueue → pManager).
//  7. leaderPClock++ → next round.
func RunConsensus(
	myPriority *smr.PriorityState,
	myState *smr.ServerState,
	pManager *smr.PriorityManager,
	numServers int,
	cmdCh <-chan KVCommand,
	localExec func(op, key, valueJSON string) error,
) {
	leaderPClock := 0

	for cmd := range cmdCh { // each iteration = one Cabinet round
		receiver := make(chan ReplyInfo, numServers)
		prioQueue := make(chan int, numServers)

		// 1. Get each follower's weight for this pClock
		fpriorities := pManager.GetFollowerPriorities(leaderPClock)
		fmt.Printf("[Cabinet] pClock=%d op=%s key=%s | majority=%.4f\n",
			leaderPClock, cmd.Op, cmd.Key, myPriority.Majority)

		// 2. Broadcast ConsensusService RPC to all followers concurrently
		conns.RLock()
		for _, conn := range conns.m {
			args := &Args{
				PrioClock: leaderPClock,
				PrioVal:   fpriorities[conn.serverID],
				Op:        cmd.Op,
				Key:       cmd.Key,
				ValueJSON: cmd.ValueJSON,
			}
			go executeRPC(conn, serviceMethod, args, receiver)
		}
		conns.RUnlock()

		// 3 & 4. Accumulate weights; first node to push prioSum over majority wins
		prioSum := myPriority.PrioVal // leader counts its own weight immediately
		timeout := time.After(5 * time.Second)
		timedOut := false

	waitLoop:
		for {
			select {
			case rinfo := <-receiver:
				// Push into prioQueue in arrival order (fastest node → index 0)
				prioQueue <- rinfo.SID

				fpriorities = pManager.GetFollowerPriorities(leaderPClock)
				prioSum += fpriorities[rinfo.SID]

				fmt.Printf("[Cabinet] pClock=%d node=%d replied | prioSum=%.4f\n",
					leaderPClock, rinfo.SID, prioSum)

				if prioSum > myPriority.Majority {
					break waitLoop // weighted quorum reached
				}

			case <-timeout:
				fmt.Printf("[Cabinet] pClock=%d timeout: quorum not reached\n", leaderPClock)
				timedOut = true
				break waitLoop
			}
		}

		// 5. Commit: apply command to leader's local MongoDB
		var execErr error
		if timedOut {
			execErr = fmt.Errorf("consensus timeout: quorum not reached")
		} else {
			execErr = localExec(cmd.Op, cmd.Key, cmd.ValueJSON)
			if execErr != nil {
				fmt.Printf("[Cabinet] pClock=%d local exec failed: %v\n", leaderPClock, execErr)
			}
		}

		// Notify the waiting HTTP handler of the commit result
		if cmd.ReplyCh != nil {
			cmd.ReplyCh <- execErr
		}

		// 6. Re-rank follower weights based on reply order this round
		leaderPClock++
		if err := pManager.UpdateFollowerPriorities(leaderPClock, prioQueue, myState.GetLeaderID()); err != nil {
			fmt.Printf("[Cabinet] UpdateFollowerPriorities failed: %v\n", err)
		}
	}
}

// executeRPC sends one RPC to a single follower.
// Restored from cabinet/consensus.go — serviceMethod is passed as a parameter.
//
// The jobQ enforces ordered delivery per follower: round N's RPC waits for
// round N-1 to complete on the same TCP connection before being sent.
func executeRPC(conn *ServerDock, serviceMethod string, args *Args, receiver chan ReplyInfo) {
	stack := make(chan struct{}, 1)

	conn.jobQMu.Lock()
	conn.jobQ[args.PrioClock] = stack
	conn.jobQMu.Unlock()

	// Wait for previous round's RPC to complete on this follower
	if args.PrioClock > 0 {
		conn.jobQMu.RLock()
		prev := conn.jobQ[args.PrioClock-1]
		conn.jobQMu.RUnlock()
		<-prev
	}

	reply := Reply{}
	err := conn.txClient.Call(serviceMethod, args, &reply)

	// Always signal the next round it can proceed (even on failure)
	conn.jobQMu.Lock()
	conn.jobQ[args.PrioClock] <- struct{}{}
	conn.jobQMu.Unlock()

	if err != nil {
		fmt.Printf("[Cabinet] RPC to node %d failed: %v\n", conn.serverID, err)
		return
	}

	receiver <- ReplyInfo{SID: conn.serverID, PClock: args.PrioClock, Recv: reply}
}
