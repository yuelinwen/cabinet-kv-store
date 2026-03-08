package cabinet

import (
	"net/rpc"
	"sync"
)

// conns holds live RPC connections to all follower nodes, keyed by serverID.
// The leader populates this via EstablishRPCs before starting RunConsensus.
var conns = struct {
	sync.RWMutex
	m map[int]*ServerDock
}{m: make(map[int]*ServerDock)}

// ServerDock holds one follower's RPC client and a per-round job queue.
// The jobQ enforces ordered delivery: round N waits for round N-1 to
// complete on the same follower before sending the next RPC.
type ServerDock struct {
	serverID int
	addr     string
	txClient *rpc.Client
	jobQMu   sync.RWMutex
	jobQ     map[int]chan struct{}
}
