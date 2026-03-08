package cabinet

// 【Follower RPC 服务】
//
// CabService 是 follower 暴露给 leader 的 RPC 接口。
// leader 每轮共识都会调用 ConsensusService，follower 收到后：
//   1. 更新自己的权重（为下一轮做准备）
//   2. 把 KV 命令写入自己的 MongoDB

import (
	"github.com/yuelinwen/cabinet-kv-store/server/cabinet/smr"
)

// Args 是 leader 每轮发给 follower 的 RPC 参数
type Args struct {
	PrioClock PrioClock // 本轮轮次编号（follower 用来更新自己的 pClock）
	PrioVal   Priority  // leader 给这个 follower 分配的新权重
	Op        string    // 操作类型："INSERT" / "REPLACE" / "DELETE"
	Key       string    // customer ID
	ValueJSON string    // 客户 JSON 数据（DELETE 时为空）
}

// Reply 是 follower 执行完后返回给 leader 的结果
type Reply struct {
	Success bool   // true = 执行成功
	ErrMsg  string // 失败原因（成功时为空）
}

// CabService 是注册到 follower RPC server 上的服务对象
// Priority   = 该 follower 自己的权重状态（每轮更新）
// KVExecutor = 执行写操作的函数（由 main.go 注入，操作该 follower 的 MongoDB）
type CabService struct {
	Priority   *smr.PriorityState
	KVExecutor func(op, key, valueJSON string) error
}

// NewCabService 创建 CabService 实例（在 startFollowerRPC 里调用）
func NewCabService(p *smr.PriorityState, exec func(op, key, valueJSON string) error) *CabService {
	return &CabService{Priority: p, KVExecutor: exec}
}

// ConsensusService 是 follower 暴露的唯一 RPC 方法，leader 通过 txClient.Call 调用
//
// 执行步骤：
//  1. 更新本节点权重到本轮的 pClock 和 PrioVal（保证单调性）
//  2. 调用 KVExecutor 把命令写入本节点的 MongoDB
//  3. 设置 reply.Success = true 告知 leader 执行成功
func (s *CabService) ConsensusService(args *Args, reply *Reply) error {
	// 步骤 1：更新权重（拒绝旧轮次的重放）
	if err := s.Priority.UpdatePriority(args.PrioClock, args.PrioVal); err != nil {
		reply.ErrMsg = err.Error()
		return err
	}
	// 步骤 2：执行写操作到本节点的 MongoDB
	if err := s.KVExecutor(args.Op, args.Key, args.ValueJSON); err != nil {
		reply.ErrMsg = err.Error()
		reply.Success = false
		return err
	}
	reply.Success = true
	return nil
}
