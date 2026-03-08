package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/cabinet"
	"github.com/yuelinwen/cabinet-kv-store/server/cabinet/smr"
	"github.com/yuelinwen/cabinet-kv-store/server/controllers"
	"github.com/yuelinwen/cabinet-kv-store/server/database"
	"github.com/yuelinwen/cabinet-kv-store/server/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ── 集群配置（第一阶段：固定 leader，暂无选举）──────────────────────────────
const (
	NumNodes         = 3    // 集群节点总数
	Tolerance        = 1    // Cabinet 容错数 t（可容忍 t 个节点故障）
	LeaderID         = 0    // 固定 leader 的节点 ID
	GatewayPort      = 8080 // 对外唯一 REST 端口
	InternalBasePort = 9080 // 各节点内部 Gin HTTP 端口：9080 / 9081 / 9082
	RPCBasePort      = 9180 // 各节点 Cabinet RPC 端口：9180 / 9181 / 9182
)

// currentLeaderID 记录 gateway 当前认为哪个节点是 leader（原子操作，并发安全）
var currentLeaderID atomic.Int32

// ════════════════════════════════════════════════════════════════════════════
// Gateway 层
// 职责：作为唯一对外入口（8080），把请求转发给内部节点
// ════════════════════════════════════════════════════════════════════════════

// 步骤 1：Gateway 入口 — 根据请求类型分流
//
//	写操作（POST/PUT/DELETE）→ 必须发给 leader（因为只有 leader 能运行共识）
//	读操作（GET）            → 发给任意存活节点（每个节点都有完整数据）
func proxyRequest(w http.ResponseWriter, r *http.Request) {
	// 步骤 1a：先把请求 body 读出来，后续转发时复用
	bodyBytes, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// 步骤 1b：写请求 → 只转发给 leader（9080）
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
		sendToNode(w, r, bodyBytes, LeaderID)
		return
	}

	// 步骤 1c：读请求 → 并发尝试所有节点，谁先回就用谁
	sendToAnyNode(w, r, bodyBytes)
}

// 步骤 2：转发给指定单节点（写操作专用）
//
//	target = http://localhost:9080<path>
//	把 leader 的响应原样返回给客户端
func sendToNode(w http.ResponseWriter, r *http.Request, bodyBytes []byte, nodeID int) {
	target := fmt.Sprintf("http://localhost:%d%s", InternalBasePort+nodeID, r.URL.RequestURI())
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(r.Method, target, bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}
	for k, v := range r.Header {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "leader unavailable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// 步骤 3：并发探测所有节点（读操作专用）
//
//	while loop：最多等 30 秒
//	  每轮同时向 Node0/1/2 发请求（3 个 goroutine）
//	  第一个成功回复的节点 → winner，其余丢弃
//	  全部失败 → 等 500ms 重试（节点可能还在启动）
func sendToAnyNode(w http.ResponseWriter, r *http.Request, bodyBytes []byte) {
	type nodeResult struct {
		resp   *http.Response
		nodeID int
	}

	deadline := time.Now().Add(30 * time.Second)

	// 步骤 3a：while loop 主体，每轮相当于一次"探测"
	for time.Now().Before(deadline) {
		ch := make(chan nodeResult, NumNodes)

		// 步骤 3b：并发向所有节点发请求
		for i := 0; i < NumNodes; i++ {
			go func(id int) {
				target := fmt.Sprintf("http://localhost:%d%s", InternalBasePort+id, r.URL.RequestURI())
				client := &http.Client{Timeout: 2 * time.Second}
				req, err := http.NewRequest(r.Method, target, bytes.NewReader(bodyBytes))
				if err != nil {
					ch <- nodeResult{nil, id}
					return
				}
				for k, v := range r.Header {
					req.Header[k] = v
				}
				resp, err := client.Do(req)
				if err != nil {
					ch <- nodeResult{nil, id}
					return
				}
				ch <- nodeResult{resp, id}
			}(i)
		}

		// 步骤 3c：收集所有 goroutine 的结果，选第一个成功的
		var winner nodeResult
		for i := 0; i < NumNodes; i++ {
			res := <-ch
			if res.resp == nil {
				continue
			}
			if winner.resp == nil {
				winner = res // 第一个成功的赢
			} else {
				res.resp.Body.Close() // 多余的响应直接关掉
			}
		}

		// 步骤 3d：有胜者 → 返回响应给客户端
		if winner.resp != nil {
			currentLeaderID.Store(int32(winner.nodeID))
			for k, v := range winner.resp.Header {
				w.Header()[k] = v
			}
			w.WriteHeader(winner.resp.StatusCode)
			io.Copy(w, winner.resp.Body)
			winner.resp.Body.Close()
			return
		}

		// 步骤 3e：全部失败 → 等 500ms 再试
		fmt.Printf("[Gateway] all nodes unreachable, retrying in 500ms...\n")
		time.Sleep(500 * time.Millisecond)
	}

	// 步骤 3f：30 秒内仍无节点响应 → 503
	http.Error(w, "503 Service Unavailable: timed out waiting for any node", http.StatusServiceUnavailable)
}

// 步骤 4：启动 Gateway HTTP 服务（对外端口 8080）
func startGateway() {
	currentLeaderID.Store(int32(LeaderID))
	fmt.Printf("[Gateway] listening on localhost:%d → internal nodes %d..%d\n",
		GatewayPort, InternalBasePort, InternalBasePort+NumNodes-1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", proxyRequest) // 所有路径都走 proxyRequest
	if err := http.ListenAndServe(fmt.Sprintf("localhost:%d", GatewayPort), mux); err != nil {
		fmt.Printf("[Gateway] error: %v\n", err)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// Cabinet 辅助函数
// ════════════════════════════════════════════════════════════════════════════

// 步骤 5：初始化该节点的 Cabinet 权重
//
//	Cabinet 用几何级数分配权重：leader 最高，越慢的 follower 权重越低
//	n=3, t=1 时：scheme = [高, 中, 低]，majority = sum/2
//	每个节点按自己的 ID 取对应初始权重
func initCabinetPriority(nodeID int) (*smr.PriorityState, *smr.PriorityManager) {
	pm := &smr.PriorityManager{}
	pm.Init(NumNodes, Tolerance+1, 10, 0.01, true) // 计算几何权重方案

	fprios := pm.GetFollowerPriorities(0)          // 取 pClock=0 时的初始权重表
	ps := smr.NewServerPriority(0, fprios[nodeID]) // 本节点的初始权重
	ps.Majority = pm.GetMajority()                 // 存入 majority 阈值
	return &ps, pm
}

// 步骤 6：生成 KV 执行器（闭包）
//
//	封装该节点的 MongoDB 操作，leader 和 follower 都用同一套逻辑
//	leader 在共识完成后调用（localExec）
//	follower 在收到 RPC 时调用（KVExecutor）
func makeKVExecutor(nodeID int) func(op, key, valueJSON string) error {
	return func(op, key, valueJSON string) error {
		col := database.GetCollection(nodeID) // 取该节点专属的 MongoDB collection
		ctx := context.TODO()
		switch op {
		case "INSERT": // 对应 HTTP POST
			var customer models.Customer
			if err := json.Unmarshal([]byte(valueJSON), &customer); err != nil {
				return err
			}
			_, err := col.InsertOne(ctx, customer)
			return err
		case "REPLACE": // 对应 HTTP PUT
			var customer models.Customer
			if err := json.Unmarshal([]byte(valueJSON), &customer); err != nil {
				return err
			}
			res, err := col.ReplaceOne(ctx, bson.M{"_id": key}, customer)
			if err != nil {
				return err
			}
			if res.MatchedCount == 0 {
				return fmt.Errorf("customer not found")
			}
			return nil
		case "DELETE": // 对应 HTTP DELETE
			res, err := col.DeleteOne(ctx, bson.M{"_id": key})
			if err != nil {
				return err
			}
			if res.DeletedCount == 0 {
				return fmt.Errorf("customer not found")
			}
			return nil
		default:
			return fmt.Errorf("unknown op: %s", op)
		}
	}
}

// 步骤 7：启动 follower 的 RPC 服务（在独立 goroutine 中运行）
//
//	注册 CabService，监听 TCP 端口（9181 或 9182）
//	leader 建立连接后，每个共识轮次都通过这条 TCP 连接发送 KV 命令
func startFollowerRPC(id int, svc *cabinet.CabService) {
	server := rpc.NewServer() // 独立 RPC server（避免同进程多节点冲突）
	if err := server.Register(svc); err != nil {
		fmt.Printf("[Node %d] RPC register error: %v\n", id, err)
		return
	}
	addr := fmt.Sprintf("localhost:%d", RPCBasePort+id) // 9181 或 9182
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("[Node %d] RPC listen error: %v\n", id, err)
		return
	}
	fmt.Printf("[Node %d] RPC server listening on %s\n", id, addr)
	server.Accept(ln) // 阻塞，持续接受 leader 的 RPC 调用
}

// ════════════════════════════════════════════════════════════════════════════
// 节点启动
// ════════════════════════════════════════════════════════════════════════════

// startNode 启动一个完整节点（leader 或 follower）
func startNode(id int) {
	role := "follower"
	if id == LeaderID {
		role = "leader"
	}
	addr := fmt.Sprintf("localhost:%d", InternalBasePort+id)
	fmt.Printf("[Node %d] starting as %s on %s\n", id, role, addr)

	// 步骤 8：连接该节点专属的 MongoDB（bank_db_0 / bank_db_1 / bank_db_2）
	database.ConnectMongoDB(id)
	defer func() {
		if err := database.DisconnectNode(id); err != nil {
			fmt.Printf("[Node %d] MongoDB disconnect error: %v\n", id, err)
		}
	}()

	// 步骤 9：如果数据库为空，从 CSV 文件导入种子数据（幂等操作）
	database.SeedDatabaseFromCSV(id)

	// 步骤 10：初始化该节点的 Cabinet 权重和 KV 执行器
	myPriority, pManager := initCabinetPriority(id)
	myState := smr.NewServerState()
	myState.SetMyServerID(id)
	myState.SetLeaderID(LeaderID)
	kvExec := makeKVExecutor(id) // 封装写 MongoDB 的函数

	if id == LeaderID {
		// ── Leader 专属步骤 ──────────────────────────────────────────────

		// 步骤 11：创建命令通道，注入给 HTTP controllers
		//   HTTP 写请求到来时，controller 把 KVCommand 塞入 cmdCh
		//   RunConsensus 从 cmdCh 取出并运行共识
		cmdCh := make(chan cabinet.KVCommand, 100)
		controllers.CmdCh = cmdCh

		go func() {
			// 步骤 12：连接所有 follower 的 RPC 端口（会重试直到全部连上）
			//   连接 9181（Node1）和 9182（Node2）
			cabinet.EstablishRPCs(id, NumNodes, RPCBasePort)

			// 步骤 13：启动 Cabinet 共识主循环（阻塞，持续消费 cmdCh）
			//   每次从 cmdCh 取一个命令 → 广播 RPC → 等 quorum → 提交 → 更新权重
			cabinet.RunConsensus(myPriority, &myState, pManager, NumNodes, cmdCh, kvExec)
		}()
	} else {
		// ── Follower 专属步骤 ────────────────────────────────────────────

		// 步骤 11（follower）：注册并启动 RPC 服务
		//   等待 leader 连接，收到命令后调用 kvExec 写本节点的 MongoDB
		svc := cabinet.NewCabService(myPriority, kvExec)
		go startFollowerRPC(id, svc)
	}

	// 步骤 14：初始化 Gin HTTP 路由
	router := gin.Default()

	// 步骤 15：中间件——把 nodeID 注入每个请求的 context
	//   controllers 通过 context 取 nodeID，再用它拿到对应的 MongoDB collection
	router.Use(func(c *gin.Context) {
		c.Set("nodeID", id)
		c.Next()
	})

	// 步骤 16：注册 REST 路由
	//   GET  → 直接读本节点 MongoDB（不走共识）
	//   写操作 → gateway 已保证只有 leader 收到，handler 内部走 CmdCh → 共识
	router.GET("/customers", controllers.GetCustomers)
	router.GET("/customers/:id", controllers.GetCustomerByID)
	router.POST("/customers", controllers.PostCustomer)
	router.PUT("/customers/:id", controllers.PutCustomerByID)
	router.DELETE("/customers/:id", controllers.DeleteCustomerByID)

	// 步骤 17：启动 Gin，监听内部端口（9080 / 9081 / 9082），阻塞
	if err := router.Run(addr); err != nil {
		fmt.Printf("[Node %d] gin error: %v\n", id, err)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// 程序入口
// ════════════════════════════════════════════════════════════════════════════

func main() {
	fmt.Printf("[Cabinet] Cluster config: n=%d, t=%d, leader=%d\n", NumNodes, Tolerance, LeaderID)

	var wg sync.WaitGroup

	// 步骤 A：启动 Gateway（端口 8080），所有外部请求的唯一入口
	wg.Add(1)
	go func() {
		defer wg.Done()
		startGateway()
	}()

	// 步骤 B：循环启动 NumNodes 个节点（每个是独立 goroutine）
	//   id=0 → leader（Gin:9080, RPC:9180）
	//   id=1 → follower（Gin:9081, RPC:9181）
	//   id=2 → follower（Gin:9082, RPC:9182）
	for id := 0; id < NumNodes; id++ {
		wg.Add(1)
		go func(nodeID int) {
			defer wg.Done()
			startNode(nodeID)
		}(id)
	}

	// 步骤 C：等待所有 goroutine（正常运行时永不退出，除非某个节点崩溃）
	wg.Wait()
}
