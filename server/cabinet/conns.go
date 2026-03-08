package cabinet

import (
	"net/rpc"
	"sync"
)

// Conns holds live RPC connections to all follower nodes, keyed by serverID.
// The leader populates this via EstablishRPCs before starting RunConsensus.
var Conns = struct {
	sync.RWMutex
	M map[int]*ServerDock
}{M: make(map[int]*ServerDock)}

// ServerDock holds one follower's RPC client and a per-round job queue.
// The jobQ enforces ordered delivery: round N waits for round N-1 to
// complete on the same follower before sending the next RPC.
type ServerDock struct {
	ServerID int
	Addr     string
	TxClient *rpc.Client
	JobQMu   sync.RWMutex
	JobQ     map[PrioClock]chan struct{}
}
