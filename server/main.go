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

// Cluster configuration (Phase 1: fixed leader, no election)
const (
	NumNodes         = 3    // total number of nodes in the cluster
	Tolerance        = 1    // Cabinet failure tolerance t
	LeaderID         = 0    // fixed leader node ID
	GatewayPort      = 8080 // single public-facing REST port
	InternalBasePort = 9080 // Gin HTTP ports: 9080, 9081, 9082
	RPCBasePort      = 9180 // Cabinet RPC ports: 9180, 9181, 9182
)

var currentLeaderID atomic.Int32

// ── Gateway ────────────────────────────────────────────────────────────────

// proxyRequest dispatches to the right forwarding strategy:
//   - POST/PUT/DELETE → leader only (writes must go through Cabinet consensus)
//   - GET             → any live node (reads are served locally on each node)
func proxyRequest(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
		// Writes: leader only
		sendToNode(w, r, bodyBytes, LeaderID)
		return
	}

	// Reads: any live node (concurrent while loop)
	sendToAnyNode(w, r, bodyBytes)
}

// sendToNode forwards a request to a single node and writes the response back.
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

// sendToAnyNode fires requests to all nodes concurrently and uses the first
// successful response. Retries every 500ms for up to 30 seconds.
func sendToAnyNode(w http.ResponseWriter, r *http.Request, bodyBytes []byte) {
	type nodeResult struct {
		resp   *http.Response
		nodeID int
	}

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		ch := make(chan nodeResult, NumNodes)

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

		var winner nodeResult
		for i := 0; i < NumNodes; i++ {
			res := <-ch
			if res.resp == nil {
				continue
			}
			if winner.resp == nil {
				winner = res
			} else {
				res.resp.Body.Close()
			}
		}

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

		fmt.Printf("[Gateway] all nodes unreachable, retrying in 500ms...\n")
		time.Sleep(500 * time.Millisecond)
	}

	http.Error(w, "503 Service Unavailable: timed out waiting for any node", http.StatusServiceUnavailable)
}

// startGateway runs the single public-facing HTTP proxy on GatewayPort.
func startGateway() {
	currentLeaderID.Store(int32(LeaderID))
	fmt.Printf("[Gateway] listening on localhost:%d → internal nodes %d..%d\n",
		GatewayPort, InternalBasePort, InternalBasePort+NumNodes-1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", proxyRequest)
	if err := http.ListenAndServe(fmt.Sprintf("localhost:%d", GatewayPort), mux); err != nil {
		fmt.Printf("[Gateway] error: %v\n", err)
	}
}

// ── Cabinet helpers ─────────────────────────────────────────────────────────

// initCabinetPriority creates a PriorityManager and returns this node's
// PriorityState (initial weight) together with the manager (needed by leader).
func initCabinetPriority(nodeID int) (*smr.PriorityState, *smr.PriorityManager) {
	pm := &smr.PriorityManager{}
	pm.Init(NumNodes, Tolerance+1, 10, 0.01, true)

	fprios := pm.GetFollowerPriorities(0)
	ps := smr.NewServerPriority(0, fprios[nodeID])
	ps.Majority = pm.GetMajority()
	return &ps, pm
}

// makeKVExecutor returns a closure that applies a Cabinet KV command to the
// given node's MongoDB collection. Used as localExec on the leader and as
// KVExecutor inside the follower's CabService RPC handler.
func makeKVExecutor(nodeID int) func(op, key, valueJSON string) error {
	return func(op, key, valueJSON string) error {
		col := database.GetCollection(nodeID)
		ctx := context.TODO()
		switch op {
		case "INSERT":
			var customer models.Customer
			if err := json.Unmarshal([]byte(valueJSON), &customer); err != nil {
				return err
			}
			_, err := col.InsertOne(ctx, customer)
			return err
		case "REPLACE":
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
		case "DELETE":
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

// startFollowerRPC registers a CabService on a dedicated RPC server and
// begins accepting leader connections. Blocks until the listener closes.
func startFollowerRPC(id int, svc *cabinet.CabService) {
	server := rpc.NewServer()
	if err := server.Register(svc); err != nil {
		fmt.Printf("[Node %d] RPC register error: %v\n", id, err)
		return
	}
	addr := fmt.Sprintf("localhost:%d", RPCBasePort+id)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("[Node %d] RPC listen error: %v\n", id, err)
		return
	}
	fmt.Printf("[Node %d] RPC server listening on %s\n", id, addr)
	server.Accept(ln)
}

// ── Node ────────────────────────────────────────────────────────────────────

// startNode initialises and runs a single Cabinet node on its internal port.
func startNode(id int) {
	role := "follower"
	if id == LeaderID {
		role = "leader"
	}
	addr := fmt.Sprintf("localhost:%d", InternalBasePort+id)
	fmt.Printf("[Node %d] starting as %s on %s\n", id, role, addr)

	// 1. Connect this node to its own MongoDB database
	database.ConnectMongoDB(id)
	defer func() {
		if err := database.DisconnectNode(id); err != nil {
			fmt.Printf("[Node %d] MongoDB disconnect error: %v\n", id, err)
		}
	}()

	// 2. Seed database from CSV if empty
	database.SeedDatabaseFromCSV(id)

	// 3. Cabinet consensus setup
	myPriority, pManager := initCabinetPriority(id)
	myState := smr.NewServerState()
	myState.SetMyServerID(id)
	myState.SetLeaderID(LeaderID)
	kvExec := makeKVExecutor(id)

	if id == LeaderID {
		// Leader: inject cmdCh into HTTP controllers, then establish RPC
		// connections to all followers and start the Cabinet consensus loop.
		cmdCh := make(chan cabinet.KVCommand, 100)
		controllers.CmdCh = cmdCh

		go func() {
			cabinet.EstablishRPCs(id, NumNodes, RPCBasePort)
			cabinet.RunConsensus(myPriority, &myState, pManager, NumNodes, cmdCh, kvExec)
		}()
	} else {
		// Follower: expose CabService RPC so the leader can replicate writes.
		svc := cabinet.NewCabService(myPriority, kvExec)
		go startFollowerRPC(id, svc)
	}

	// 4. Initialise Gin router
	router := gin.Default()

	// Inject nodeID so controllers look up the correct MongoDB collection.
	router.Use(func(c *gin.Context) {
		c.Set("nodeID", id)
		c.Next()
	})
	router.GET("/customers", controllers.GetCustomers)
	router.GET("/customers/:id", controllers.GetCustomerByID)
	router.POST("/customers", controllers.PostCustomer)
	router.PUT("/customers/:id", controllers.PutCustomerByID)
	router.DELETE("/customers/:id", controllers.DeleteCustomerByID)

	// 5. Start listening on internal port
	if err := router.Run(addr); err != nil {
		fmt.Printf("[Node %d] gin error: %v\n", id, err)
	}
}

func main() {
	fmt.Printf("[Cabinet] Cluster config: n=%d, t=%d, leader=%d\n", NumNodes, Tolerance, LeaderID)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		startGateway()
	}()

	for id := 0; id < NumNodes; id++ {
		wg.Add(1)
		go func(nodeID int) {
			defer wg.Done()
			startNode(nodeID)
		}(id)
	}

	wg.Wait()
}
