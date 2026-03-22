package smr

import "sync"

// ServerState holds node identity and Raft-style bookkeeping fields.
type ServerState struct {
	sync.RWMutex
	myID         int
	leaderID     int
	term         int
	votedForTerm int // the term in which this node last cast a vote
	votedForID   int // the candidate that received the vote (-1 = none)
	logIndex     int
	cmtIndex     int
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

// Term / election fields
func (s *ServerState) SetTerm(term int) { s.Lock(); defer s.Unlock(); s.term = term }
func (s *ServerState) GetTerm() int     { return s.term }

// TryVote grants a vote to candidateID for the given term.
// Returns true if the vote was granted (first vote this term).
// Returns false if we already voted this term.
func (s *ServerState) TryVote(term, candidateID int) bool {
	s.Lock()
	defer s.Unlock()
	if term > s.votedForTerm {
		s.votedForTerm = term
		s.votedForID = candidateID
		return true
	}
	return false
}

// Log / commit index
func (s *ServerState) AddCommitIndex(n int) { s.Lock(); defer s.Unlock(); s.cmtIndex += n }
func (s *ServerState) GetCommitIndex() int  { return s.cmtIndex }
func (s *ServerState) AddLogIndex(n int)    { s.Lock(); defer s.Unlock(); s.logIndex += n }
func (s *ServerState) GetLogIndex() int     { return s.logIndex }
