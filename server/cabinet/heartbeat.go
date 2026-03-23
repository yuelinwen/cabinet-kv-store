package cabinet

import (
	"fmt"
	"sync/atomic"
	"time"
)

const (
	heartbeatMethod   = "CabService.Heartbeat"
	HeartbeatInterval = 150 * time.Millisecond
	HeartbeatTimeout  = 500 * time.Millisecond
)

// RunHeartbeat is called by the leader to broadcast heartbeats every HeartbeatInterval.
// term is the leader's current election term (0 for the initial fixed leader).
// Runs forever (until the process exits).
func RunHeartbeat(leaderID, term int) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		conns.RLock()
		for _, conn := range conns.m {
			go sendHeartbeat(conn, leaderID, term)
		}
		conns.RUnlock()
	}
}

var beatCount atomic.Int64

func sendHeartbeat(conn *ServerDock, leaderID, term int) {
	args := &HeartbeatArgs{LeaderID: leaderID, Term: term}
	reply := &HeartbeatReply{}
	if err := conn.txClient.Call(heartbeatMethod, args, reply); err != nil {
		fmt.Printf("[Node %d | Leader    | RPC ] heartbeat → node %d failed: %v\n", leaderID, conn.serverID, err)
		return
	}
	n := beatCount.Add(1)
	if n%10 == 0 {
		fmt.Printf("[Node %d | Leader    | RPC ] heartbeat → node %d ok (beat #%d)\n", leaderID, conn.serverID, n)
	}
}
