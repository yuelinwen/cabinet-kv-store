package cabinet

import (
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"
)

// tryConnectMissingPeers dials any peer that is not yet in conns.m.
// Called periodically by the leader's heartbeat loop to pick up nodes that
// were not running when EstablishRPCsBestEffort ran (late-joining peers).
func tryConnectMissingPeers(myServerID, numServers, rpcBasePort int) {
	for i := 0; i < numServers; i++ {
		if i == myServerID {
			continue
		}
		conns.RLock()
		_, exists := conns.m[i]
		conns.RUnlock()
		if exists {
			continue
		}
		addr := fmt.Sprintf("localhost:%d", rpcBasePort+i)
		raw, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err != nil {
			continue
		}
		txClient := rpc.NewClient(raw)
		conns.Lock()
		conns.m[i] = &ServerDock{
			serverID: i,
			addr:     addr,
			txClient: txClient,
			jobQMu:   sync.RWMutex{},
			jobQ:     map[int]chan struct{}{},
		}
		conns.Unlock()
		fmt.Printf("[Node %d | Leader    | RPC ] reconnected to late-joining node %d\n", myServerID, i)
	}
}

// EstablishRPCsBestEffort dials each peer once with a 2-second timeout per node.
// Unreachable nodes are skipped (no infinite retry). Returns after all attempts
// complete. Used by a newly elected leader so it can start heartbeats quickly
// without waiting forever for nodes that may be dead.
func EstablishRPCsBestEffort(myServerID, numServers, rpcBasePort int) {
	var wg sync.WaitGroup
	for i := 0; i < numServers; i++ {
		if i == myServerID {
			continue
		}
		wg.Add(1)
		go func(peerID int) {
			defer wg.Done()
			addr := fmt.Sprintf("localhost:%d", rpcBasePort+peerID)
			raw, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				fmt.Printf("[Node %d | Leader    | RPC ] node %d unreachable, skipped\n", myServerID, peerID)
				return
			}
			txClient := rpc.NewClient(raw)
			conns.Lock()
			conns.m[peerID] = &ServerDock{
				serverID: peerID,
				addr:     addr,
				txClient: txClient,
				jobQMu:   sync.RWMutex{},
				jobQ:     map[int]chan struct{}{},
			}
			conns.Unlock()
			fmt.Printf("[Node %d | Leader    | RPC ] connected to node %d\n", myServerID, peerID)
		}(i)
	}
	wg.Wait()
}

// EstablishRPCs dials the RPC port of every follower and stores connections in conns.
// Adapted from cabinet/primary.go — config-file parsing replaced with direct port
// arithmetic: follower i listens for RPC on localhost:(rpcBasePort+i).
//
// Retries every second until all followers are reachable (they may still be
// initialising MongoDB when the leader calls this).
func EstablishRPCs(myServerID, numServers, rpcBasePort int) {
	for i := 0; i < numServers; i++ {
		if i == myServerID {
			continue
		}

		addr := fmt.Sprintf("localhost:%d", rpcBasePort+i)
		fmt.Printf("[Node %d | Leader    | RPC ] connecting to node %d at %s...\n", myServerID, i, addr)

		var txClient *rpc.Client
		var err error
		for {
			txClient, err = rpc.Dial("tcp", addr)
			if err == nil {
				break
			}
			fmt.Printf("[Node %d | Leader    | RPC ] node %d not ready, retrying...\n", myServerID, i)
			time.Sleep(100 * time.Millisecond)
		}

		fmt.Printf("[Node %d | Leader    | RPC ] connected to node %d\n", myServerID, i)

		conns.Lock()
		conns.m[i] = &ServerDock{
			serverID: i,
			addr:     addr,
			txClient: txClient,
			jobQMu:   sync.RWMutex{},
			jobQ:     map[int]chan struct{}{},
		}
		conns.Unlock()
	}
}
