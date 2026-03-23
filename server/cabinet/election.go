package cabinet

import (
	"fmt"
	"math/rand"
	"net/rpc"
	"sync"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/cabinet/smr"
)

const (
	requestVoteMethod  = "CabService.RequestVote"
	electionMinTimeout = 150 * time.Millisecond
	electionMaxTimeout = 300 * time.Millisecond
)

// StartElection runs a Raft-style election from nodeID's perspective.
// Election quorum = numNodes - tolerance (Cabinet §4.1.3).
// Returns true if this node won and should become the new leader.
func StartElection(nodeID, numNodes, tolerance, rpcBasePort int, state *smr.ServerState) bool {
	// Random jitter (150–300 ms) to reduce split votes when multiple nodes
	// detect the timeout at the same time.
	jitter := time.Duration(rand.Int63n(int64(electionMaxTimeout - electionMinTimeout)))
	time.Sleep(electionMinTimeout + jitter)

	newTerm := state.GetTerm() + 1
	state.SetTerm(newTerm)
	state.TryVote(newTerm, nodeID) // vote for self

	quorum := numNodes - tolerance
	votes := 1 // self-vote counts immediately

	fmt.Printf("[Node %d | Candidate | RPC ] election term %d: need %d/%d votes\n",
		nodeID, newTerm, quorum, numNodes)

	var mu sync.Mutex
	var wg sync.WaitGroup

	args := &RequestVoteArgs{Term: newTerm, CandidateID: nodeID}

	for i := 0; i < numNodes; i++ {
		if i == nodeID {
			continue
		}
		wg.Add(1)
		go func(peerID int) {
			defer wg.Done()
			addr := fmt.Sprintf("localhost:%d", rpcBasePort+peerID)
			client, err := rpc.Dial("tcp", addr)
			if err != nil {
				fmt.Printf("[Node %d | Candidate | RPC ] node %d unreachable: %v\n", nodeID, peerID, err)
				return
			}
			defer client.Close()

			reply := &RequestVoteReply{}
			if err := client.Call(requestVoteMethod, args, reply); err != nil {
				fmt.Printf("[Node %d | Candidate | RPC ] RequestVote to node %d failed: %v\n", nodeID, peerID, err)
				return
			}
			if reply.VoteGranted {
				mu.Lock()
				votes++
				mu.Unlock()
				fmt.Printf("[Node %d | Candidate | RPC ] +vote from node %d\n", nodeID, peerID)
			} else {
				fmt.Printf("[Node %d | Candidate | RPC ] vote denied by node %d (their term %d)\n",
					nodeID, peerID, reply.Term)
			}
		}(i)
	}

	wg.Wait()

	if votes >= quorum {
		fmt.Printf("[Node %d | Leader    | RPC ] won election term %d (%d/%d votes) — now LEADER\n",
			nodeID, newTerm, votes, numNodes)
		state.SetLeaderID(nodeID)
		return true
	}

	fmt.Printf("[Node %d | Candidate | RPC ] lost election term %d (%d/%d votes)\n",
		nodeID, newTerm, votes, numNodes)
	return false
}
