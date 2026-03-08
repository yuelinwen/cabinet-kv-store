package cabinet

// 【Cabinet 共识主循环】
//
// RunConsensus 是整个系统的核心，运行在 leader 节点上。
// 它从 cmdCh 中一条一条取出写命令，每条命令走一个完整的共识轮次（pClock）。
//
// 一个轮次的流程：
//  1. 从 pManager 取出本轮各 follower 的权重
//  2. 并发向所有 follower 发 RPC（带命令 + 权重）
//  3. follower 收到后立即写自己的 MongoDB，然后回复 leader
//  4. leader 按回复顺序把 follower ID 放入 prioQueue，同时累加权重
//  5. 当累计权重 > majority（加权 quorum）→ 提交：leader 也写自己的 MongoDB
//  6. 通过 ReplyCh 通知 HTTP handler 结果（成功/失败）
//  7. 根据本轮回复顺序更新下一轮权重，pClock+1

import (
	"fmt"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/cabinet/smr"
)

// serviceMethod 是 follower 注册的 RPC 方法名（固定格式：结构体名.方法名）
const serviceMethod = "CabService.ConsensusService"

// RunConsensus 共识主循环，阻塞运行，直到 cmdCh 被关闭
// myPriority = leader 自身的权重状态（pClock + PrioVal + Majority）
// myState    = leader 的节点状态（用于权重更新时传 leaderID）
// pManager   = 权重管理器（管理每轮各节点权重）
// numServers = 集群节点总数
// cmdCh      = HTTP handler 塞写命令的通道，每次取一条处理
// localExec  = leader 提交时写自己 MongoDB 的函数
func RunConsensus(
	myPriority *smr.PriorityState,
	myState *smr.ServerState,
	pManager *smr.PriorityManager,
	numServers int,
	cmdCh <-chan KVCommand,
	localExec func(op, key, valueJSON string) error,
) {
	leaderPClock := 0

	// 每次循环 = 一个完整的共识轮次
	for cmd := range cmdCh {
		// receiver：收集所有 follower 的回复
		// prioQueue：按回复先后顺序记录 follower ID（用于下一轮权重排名）
		receiver := make(chan ReplyInfo, numServers)
		prioQueue := make(chan int, numServers)

		// 步骤 1：取本轮各 follower 的权重
		fpriorities := pManager.GetFollowerPriorities(leaderPClock)
		fmt.Printf("[Cabinet] pClock=%d op=%s key=%s | majority=%.4f\n",
			leaderPClock, cmd.Op, cmd.Key, myPriority.Majority)

		// 步骤 2：并发向所有 follower 发 RPC
		conns.RLock()
		for _, conn := range conns.m {
			args := &Args{
				PrioClock: leaderPClock,
				PrioVal:   fpriorities[conn.serverID], // 告诉 follower 本轮权重
				Op:        cmd.Op,
				Key:       cmd.Key,
				ValueJSON: cmd.ValueJSON,
			}
			go executeRPC(conn, serviceMethod, args, receiver) // 每个 follower 独立 goroutine
		}
		conns.RUnlock()

		// 步骤 3 & 4：累积权重，等待加权 quorum
		prioSum := myPriority.PrioVal // leader 先把自己的权重计入
		timeout := time.After(5 * time.Second)
		timedOut := false

	waitLoop:
		for {
			select {
			case rinfo := <-receiver:
				// 把回复的 follower ID 按顺序放入 prioQueue（越快 = 越靠前）
				prioQueue <- rinfo.SID

				fpriorities = pManager.GetFollowerPriorities(leaderPClock)
				prioSum += fpriorities[rinfo.SID] // 累加该 follower 的权重

				fmt.Printf("[Cabinet] pClock=%d node=%d replied | prioSum=%.4f\n",
					leaderPClock, rinfo.SID, prioSum)

				if prioSum > myPriority.Majority {
					break waitLoop // 加权 quorum 达成，可以提交
				}

			case <-timeout:
				// 5 秒内没有足够节点回复 → 本轮超时，不提交
				fmt.Printf("[Cabinet] pClock=%d timeout: quorum not reached\n", leaderPClock)
				timedOut = true
				break waitLoop
			}
		}

		// 步骤 5：提交或报错
		var execErr error
		if timedOut {
			execErr = fmt.Errorf("consensus timeout: quorum not reached")
		} else {
			// quorum 达成 → leader 把命令写入自己的 MongoDB
			execErr = localExec(cmd.Op, cmd.Key, cmd.ValueJSON)
			if execErr != nil {
				fmt.Printf("[Cabinet] pClock=%d local exec failed: %v\n", leaderPClock, execErr)
			}
		}

		// 步骤 6：通知等待的 HTTP handler（解除 submitWrite 中的阻塞）
		if cmd.ReplyCh != nil {
			cmd.ReplyCh <- execErr
		}

		// 步骤 7：根据本轮回复顺序更新下一轮权重，然后推进 pClock
		leaderPClock++
		if err := pManager.UpdateFollowerPriorities(leaderPClock, prioQueue, myState.GetLeaderID()); err != nil {
			fmt.Printf("[Cabinet] UpdateFollowerPriorities failed: %v\n", err)
		}
	}
}

// executeRPC 向单个 follower 发一条 RPC，在独立 goroutine 中运行
//
// jobQ 保序机制：
//   同一个 follower 的 RPC 必须按轮次顺序到达，
//   第 N 轮的 RPC 要等第 N-1 轮完成（收到 jobQ[N-1] 信号）才能发出，
//   防止网络乱序导致 follower 执行错误顺序的命令。
func executeRPC(conn *ServerDock, serviceMethod string, args *Args, receiver chan ReplyInfo) {
	stack := make(chan struct{}, 1)

	// 把本轮的完成信号 channel 注册到 jobQ
	conn.jobQMu.Lock()
	conn.jobQ[args.PrioClock] = stack
	conn.jobQMu.Unlock()

	// 等上一轮（pClock-1）在这个 follower 上完成
	if args.PrioClock > 0 {
		conn.jobQMu.RLock()
		prev := conn.jobQ[args.PrioClock-1]
		conn.jobQMu.RUnlock()
		<-prev // 阻塞，直到上一轮发出完成信号
	}

	// 发出 RPC 调用（同步等待 follower 回复）
	reply := Reply{}
	err := conn.txClient.Call(serviceMethod, args, &reply)

	// 不管成功失败，都通知下一轮可以发了
	conn.jobQMu.Lock()
	conn.jobQ[args.PrioClock] <- struct{}{}
	conn.jobQMu.Unlock()

	if err != nil {
		fmt.Printf("[Cabinet] RPC to node %d failed: %v\n", conn.serverID, err)
		return // 不推入 receiver，leader 就不会累计这个节点的权重
	}

	// 成功 → 把 follower ID 和轮次推入 receiver，leader 累计权重
	receiver <- ReplyInfo{SID: conn.serverID, PClock: args.PrioClock, Recv: reply}
}
