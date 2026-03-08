package cabinet

// 【RPC 连接池】
// leader 在启动时通过 EstablishRPCs 连接每个 follower 的 TCP 端口
// 连接建立后存在 conns.m 里，RunConsensus 每轮都从这里取连接发 RPC

import (
	"net/rpc"
	"sync"
)

// conns 存 leader 到所有 follower 的 RPC 连接，key = follower 的 serverID
// sync.RWMutex 保证并发安全（读可以并发，写互斥）
var conns = struct {
	sync.RWMutex
	m map[int]*ServerDock
}{m: make(map[int]*ServerDock)}

// ServerDock 持有一个 follower 的完整连接信息
type ServerDock struct {
	serverID int              // follower 节点编号
	addr     string           // 连接地址，如 "localhost:9181"
	txClient *rpc.Client      // TCP RPC 客户端，用来调用 ConsensusService
	jobQMu   sync.RWMutex     // 保护下面的 jobQ
	jobQ     map[int]chan struct{} // 每轮的完成信号：jobQ[pClock] 完成后通知下一轮可以发送
	// jobQ 作用：保证同一 follower 的 RPC 按轮次顺序执行，第 N 轮等第 N-1 轮完成再发
}
