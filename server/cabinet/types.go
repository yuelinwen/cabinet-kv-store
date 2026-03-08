package cabinet

// 【Cabinet 基础类型定义】
// 这里定义共识层用到的所有数据结构

// 类型别名，让代码语义更清晰
type (
	ServerID  = int     // 节点编号（0=leader, 1/2=follower）
	PrioClock = int     // 共识轮次计数器（每完成一轮 +1）
	Priority  = float64 // 节点权重值（越高表示越优先）
)

// KVCommand 是一条需要通过 Cabinet 共识复制到所有节点的写命令
// HTTP handler 把写请求封装成 KVCommand，塞入 leader 的 cmdCh
type KVCommand struct {
	Op        string    // 操作类型："INSERT"（新建）/ "REPLACE"（更新）/ "DELETE"（删除）
	Key       string    // 操作目标的 customer ID
	ValueJSON string    // 客户数据的 JSON 字符串（DELETE 时为空）
	ReplyCh   chan error // 共识完成后通知 HTTP handler：nil=成功，error=失败
}

// ReplyInfo 是 follower 回复 leader 后，leader 内部用来累计权重的结构
// 每个 follower 完成执行后发一条 ReplyInfo 到 receiver channel
type ReplyInfo struct {
	SID    ServerID  // 哪个节点回复的
	PClock PrioClock // 本轮的 pClock 编号
	Recv   Reply     // 回复内容（成功/失败）
}
