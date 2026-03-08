package smr

// 【节点状态】
// 每个节点（leader 和 follower）都维护一个 ServerState
// 记录自己的 ID、leader 是谁、以及日志进度

import "sync"

// ServerState 存储节点的身份信息和日志索引
// sync.RWMutex 保证多 goroutine 并发读写安全
type ServerState struct {
	sync.RWMutex
	myID     int  // 本节点 ID
	leaderID int  // 当前 leader 的 ID（第一阶段固定为 0）
	term     int  // 选举任期（第一阶段未使用，预留给 Plan B 选举）
	votedFor bool // 是否已投票（第一阶段未使用）
	logIndex int  // 日志最新索引
	cmtIndex int  // 已提交索引
}

// NewServerState 创建一个空的节点状态
func NewServerState() ServerState {
	return ServerState{}
}

// 设置/读取本节点 ID
func (s *ServerState) SetMyServerID(id int) { s.Lock(); defer s.Unlock(); s.myID = id }
func (s *ServerState) GetMyServerID() int   { return s.myID }

// 设置/读取 leader ID（共识层用来判断谁的权重最高）
func (s *ServerState) SetLeaderID(id int) { s.Lock(); defer s.Unlock(); s.leaderID = id }
func (s *ServerState) GetLeaderID() int   { return s.leaderID }

// 以下字段第一阶段未使用，预留给 Plan B（leader 选举）
func (s *ServerState) SetTerm(term int)    { s.Lock(); defer s.Unlock(); s.term = term }
func (s *ServerState) GetTerm() int        { return s.term }
func (s *ServerState) ResetVotedFor()      { s.Lock(); defer s.Unlock(); s.votedFor = false }
func (s *ServerState) CheckVotedFor() bool { return s.votedFor }

// 日志/提交索引的增减和读取
func (s *ServerState) AddCommitIndex(n int) { s.Lock(); defer s.Unlock(); s.cmtIndex += n }
func (s *ServerState) GetCommitIndex() int  { return s.cmtIndex }
func (s *ServerState) AddLogIndex(n int)    { s.Lock(); defer s.Unlock(); s.logIndex += n }
func (s *ServerState) GetLogIndex() int     { return s.logIndex }
