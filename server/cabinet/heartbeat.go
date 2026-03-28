package cabinet

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	heartbeatMethod   = "CabService.Heartbeat"
	HeartbeatInterval = 150 * time.Millisecond // heartbeat interval for the leader to send heartbeats; should be < heartbeat timeout so followers don't falsely detect failure
	HeartbeatTimeout  = 500 * time.Millisecond // heartbeat timeout for followers to detect leader failure; should be > heartbeat interval + network delay
	HeartbeatLogEvery = 10                     // print leader heartbeat success once every N sends
)

// RunHeartbeat is called by the leader to broadcast heartbeats every HeartbeatInterval.
// term is the leader's current election term (0 for the initial fixed leader).
// onDeposed is called once if any follower rejects a heartbeat (reply.Success=false),
// meaning a higher-term leader has been elected. RunHeartbeat stops after calling onDeposed.
// numServers and rpcBasePort are used to reconnect to peers that joined after this
// leader won its election (e.g. node that was down during EstablishRPCsBestEffort).
func RunHeartbeat(leaderID, term, numServers, rpcBasePort int, onDeposed func()) {
	stopCh := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(stopCh)
			if onDeposed != nil {
				onDeposed()
			}
		})
	}

	ticker := time.NewTicker(HeartbeatInterval)
	reconnectTicker := time.NewTicker(HeartbeatInterval * 5) // every ~750 ms
	defer ticker.Stop()
	defer reconnectTicker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-reconnectTicker.C:
			go tryConnectMissingPeers(leaderID, numServers, rpcBasePort)
		case <-ticker.C:
			conns.RLock()
			for _, conn := range conns.m {
				go sendHeartbeat(conn, leaderID, term, stop)
			}
			conns.RUnlock()
		}
	}
}

var beatCount atomic.Int64

func sendHeartbeat(conn *ServerDock, leaderID, term int, onDeposed func()) {
	args := &HeartbeatArgs{LeaderID: leaderID, Term: term}
	reply := &HeartbeatReply{}
	if err := conn.txClient.Call(heartbeatMethod, args, reply); err != nil {
		fmt.Printf("[Node %d | Leader    | RPC ] heartbeat → node %d failed: %v\n", leaderID, conn.serverID, err)
		return
	}
	if !reply.Success {
		fmt.Printf("[Node %d | Leader    | RPC ] heartbeat rejected by node %d — new leader elected, stepping down\n", leaderID, conn.serverID)
		onDeposed()
		return
	}
	n := beatCount.Add(1)
	if n%HeartbeatLogEvery == 0 {
		fmt.Printf("[Node %d | Leader    | HB  ] ✓ sent → node %d ok | beat #%d | interval ~%dms\n",
			leaderID, conn.serverID, n, HeartbeatInterval.Milliseconds())
	}
}
