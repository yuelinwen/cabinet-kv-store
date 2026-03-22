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

// RunHeartbeat is called by the leader after EstablishRPCs.
// It broadcasts a Heartbeat RPC to every follower every HeartbeatInterval.
// Runs forever (until the process exits).
func RunHeartbeat(leaderID int) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		conns.RLock()
		for _, conn := range conns.m {
			go sendHeartbeat(conn, leaderID)
		}
		conns.RUnlock()
	}
}

var beatCount atomic.Int64

func sendHeartbeat(conn *ServerDock, leaderID int) {
	args := &HeartbeatArgs{LeaderID: leaderID}
	reply := &HeartbeatReply{}
	if err := conn.txClient.Call(heartbeatMethod, args, reply); err != nil {
		fmt.Printf("[Heartbeat] node %d unreachable: %v\n", conn.serverID, err)
		return
	}
	n := beatCount.Add(1)
	if n%10 == 0 {
		fmt.Printf("[Heartbeat] leader → node %d ok (beat #%d)\n", conn.serverID, n)
	}
}
