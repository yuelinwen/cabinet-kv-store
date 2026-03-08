package cabinet

// Type aliases matching Cabinet paper terminology.
type (
	ServerID  = int
	PrioClock = int
	Priority  = float64
)

// KVCommand is a single write operation to be replicated via Cabinet consensus.
type KVCommand struct {
	Op        string    // "INSERT", "REPLACE", or "DELETE"
	Key       string    // customer ID
	ValueJSON string    // JSON-encoded customer (empty for DELETE)
	ReplyCh   chan error // result sent back to the HTTP handler after commit; may be nil
}

// ReplyInfo carries a follower's response back to the leader's consensus loop.
type ReplyInfo struct {
	SID    ServerID
	PClock PrioClock
	Recv   Reply
}
