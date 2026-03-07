package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/controllers"
	"github.com/yuelinwen/cabinet-kv-store/server/database"

	"github.com/gin-gonic/gin"
)

// Cluster configuration (Phase 1: fixed leader, no election)
const (
	NumNodes         = 3    // total number of nodes in the cluster
	Tolerance        = 1    // Cabinet failure tolerance t (can survive t node failures)
	LeaderID         = 0    // initial leader node ID
	GatewayPort      = 8080 // single public-facing REST port
	InternalBasePort = 9080 // internal node ports: 9080, 9081, 9082
)

// currentLeaderID is the node the gateway currently routes requests to.
var currentLeaderID atomic.Int32

// proxyRequest forwards the request to the current leader.
// If that node is unreachable it walks through all nodes until one responds.
func proxyRequest(w http.ResponseWriter, r *http.Request) {
	// Buffer body so we can retry on a different node if needed.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	startLeader := int(currentLeaderID.Load())

	for i := 0; i < NumNodes; i++ {
		nodeID := (startLeader + i) % NumNodes
		target := fmt.Sprintf("http://localhost:%d%s", InternalBasePort+nodeID, r.URL.RequestURI())

		req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(bodyBytes))
		if err != nil {
			continue
		}
		// Copy original headers (Content-Type etc.)
		for k, v := range r.Header {
			req.Header[k] = v
		}

		resp, err := client.Do(req)
		if err != nil {
			// This node is unreachable — advance the leader pointer and try next
			fmt.Printf("[Gateway] Node %d unreachable (%v), trying next...\n", nodeID, err)
			currentLeaderID.Store(int32((nodeID + 1) % NumNodes))
			continue
		}
		defer resp.Body.Close()

		// Successful — record this node as current leader and stream response back
		currentLeaderID.Store(int32(nodeID))
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	http.Error(w, "503 Service Unavailable: all nodes unreachable", http.StatusServiceUnavailable)
}

// startGateway runs the single public-facing HTTP proxy on GatewayPort.
func startGateway() {
	currentLeaderID.Store(int32(LeaderID))
	fmt.Printf("[Gateway] listening on localhost:%d → internal nodes %d..%d (initial leader: node %d)\n",
		GatewayPort, InternalBasePort, InternalBasePort+NumNodes-1, LeaderID)

	mux := http.NewServeMux()
	mux.HandleFunc("/", proxyRequest)
	if err := http.ListenAndServe(fmt.Sprintf("localhost:%d", GatewayPort), mux); err != nil {
		fmt.Printf("[Gateway] error: %v\n", err)
	}
}

// startNode initialises and runs a single Cabinet node on its internal port.
func startNode(id int) {
	role := "follower"
	if id == LeaderID {
		role = "leader"
	}
	addr := fmt.Sprintf("localhost:%d", InternalBasePort+id)
	fmt.Printf("[Node %d] starting as %s on %s\n", id, role, addr)

	// 1. Connect this node to MongoDB
	database.ConnectMongoDB(id)
	defer func() {
		if err := database.DisconnectNode(id); err != nil {
			fmt.Printf("[Node %d] MongoDB disconnect error: %v\n", id, err)
		}
	}()

	// 2. Seed database if empty
	database.SeedDatabaseFromCSV(id)

	// 3. Initialise Gin router
	router := gin.Default()

	// Middleware: inject this node's ID into every request context so
	// controllers can look up the correct MongoDB collection.
	router.Use(func(c *gin.Context) {
		c.Set("nodeID", id)
		c.Next()
	})
	router.GET("/customers", controllers.GetCustomers)
	router.GET("/customers/:id", controllers.GetCustomerByID)
	router.POST("/customers", controllers.PostCustomer)
	router.PUT("/customers/:id", controllers.PutCustomerByID)
	router.DELETE("/customers/:id", controllers.DeleteCustomerByID)

	// 4. Start listening on internal port
	if err := router.Run(addr); err != nil {
		fmt.Printf("[Node %d] gin error: %v\n", id, err)
	}
}

func main() {
	fmt.Printf("[Cabinet] Cluster config: n=%d, t=%d, initial leader=%d\n", NumNodes, Tolerance, LeaderID)

	var wg sync.WaitGroup

	// Start public-facing gateway on port 8080
	wg.Add(1)
	go func() {
		defer wg.Done()
		startGateway()
	}()

	// Launch one internal node per cluster member
	for id := 0; id < NumNodes; id++ {
		wg.Add(1)
		go func(nodeID int) {
			defer wg.Done()
			startNode(nodeID)
		}(id)
	}

	// Block until all goroutines exit (they won't unless they crash)
	wg.Wait()
}
