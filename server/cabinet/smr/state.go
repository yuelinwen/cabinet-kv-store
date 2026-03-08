package smr

import "sync"

// ServerState holds node identity and Raft-style bookkeeping fields.
// Note: term and votedFor are placeholders for leader election (Plan B).
// They are not used in Phase 1 (fixed leader).
type ServerState struct {
	sync.RWMutex
	myID     int
	leaderID int
	term     int
	votedFor bool
	logIndex int
	cmtIndex int
}

func NewServerState() ServerState {
	return ServerState{}
}

func (s *ServerState) SetMyServerID(id int) {
	s.Lock(); defer s.Unlock()
	s.myID = id
}
func (s *ServerState) GetMyServerID() int {
	return s.myID
}

func (s *ServerState) SetLeaderID(id int) {
	s.Lock(); defer s.Unlock()
	s.leaderID = id
}
func (s *ServerState) GetLeaderID() int {
	return s.leaderID
}

// Term / election fields (unused in Phase 1)
func (s *ServerState) SetTerm(term int) { s.Lock(); defer s.Unlock(); s.term = term }
func (s *ServerState) GetTerm() int     { return s.term }
func (s *ServerState) ResetVotedFor()   { s.Lock(); defer s.Unlock(); s.votedFor = false }
func (s *ServerState) CheckVotedFor() bool { return s.votedFor }

// Log / commit index
func (s *ServerState) AddCommitIndex(n int) { s.Lock(); defer s.Unlock(); s.cmtIndex += n }
func (s *ServerState) GetCommitIndex() int  { return s.cmtIndex }
func (s *ServerState) AddLogIndex(n int)    { s.Lock(); defer s.Unlock(); s.logIndex += n }
func (s *ServerState) GetLogIndex() int     { return s.logIndex }
