package cabinet

// 【Leader 建立 RPC 连接】
//
// leader 启动后调用 EstablishRPCs，主动连接每个 follower 的 TCP RPC 端口。
// 连接建立后存入 conns.m，之后 RunConsensus 每轮通过这些连接发 RPC。

import (
	"fmt"
	"net/rpc"
	"sync"
	"time"
)

// EstablishRPCs 逐一连接所有 follower 的 RPC 端口
// myServerID  = 自己的 ID（跳过，不连自己）
// numServers  = 集群节点总数
// rpcBasePort = RPC 端口基数（9180），follower i 的端口 = 9180+i
//
// 如果 follower 还没启动，会每秒重试，直到连上为止
func EstablishRPCs(myServerID, numServers, rpcBasePort int) {
	for i := 0; i < numServers; i++ {
		if i == myServerID {
			continue // 跳过自己
		}

		addr := fmt.Sprintf("localhost:%d", rpcBasePort+i)
		fmt.Printf("[Cabinet] connecting to node %d at %s...\n", i, addr)

		// 重试循环：follower 可能还在初始化 MongoDB，需要等它就绪
		var txClient *rpc.Client
		var err error
		for {
			txClient, err = rpc.Dial("tcp", addr) // 建立 TCP 连接
			if err == nil {
				break // 连接成功，退出重试
			}
			fmt.Printf("[Cabinet] node %d not ready, retrying...\n", i)
			time.Sleep(time.Second)
		}

		fmt.Printf("[Cabinet] connected to node %d\n", i)

		// 把连接存入全局连接池
		conns.Lock()
		conns.m[i] = &ServerDock{
			serverID: i,
			addr:     addr,
			txClient: txClient,   // 这条 TCP 连接用来后续发所有 RPC
			jobQMu:   sync.RWMutex{},
			jobQ:     map[int]chan struct{}{}, // 初始化有序投递队列
		}
		conns.Unlock()
	}
}
