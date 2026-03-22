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

// HeartbeatArgs is the payload the leader sends in each heartbeat.
type HeartbeatArgs struct {
	LeaderID int
	Term     int // sender's current term; follower rejects if Term < its own term
}

// HeartbeatReply is the follower's acknowledgement of a heartbeat.
type HeartbeatReply struct {
	Success bool
}

// RequestVoteArgs is the payload a candidate sends when requesting votes.
type RequestVoteArgs struct {
	Term        int
	CandidateID int
}

// RequestVoteReply is a peer's response to a vote request.
type RequestVoteReply struct {
	Term        int  // responder's current term (so candidate can update if stale)
	VoteGranted bool
}
