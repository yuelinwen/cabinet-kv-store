package cabinet

import (
	"fmt"
	"net/rpc"
	"sync"
	"time"
)

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
		fmt.Printf("[Cabinet] connecting to node %d at %s...\n", i, addr)

		var txClient *rpc.Client
		var err error
		for {
			txClient, err = rpc.Dial("tcp", addr)
			if err == nil {
				break
			}
			fmt.Printf("[Cabinet] node %d not ready, retrying...\n", i)
			time.Sleep(time.Second)
		}

		fmt.Printf("[Cabinet] connected to node %d\n", i)

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
