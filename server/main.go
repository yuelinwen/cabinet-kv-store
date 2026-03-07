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

// proxyRequest forwards the request to any live node.
// It fires requests to all nodes concurrently and uses the first successful response.
// If all nodes are unreachable it retries every 500ms until a 30-second deadline.
func proxyRequest(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	type nodeResult struct {
		resp   *http.Response
		nodeID int
	}

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		ch := make(chan nodeResult, NumNodes)

		// Fire requests to all nodes concurrently
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

		// Collect all results; use the first successful response
		var winner nodeResult
		for i := 0; i < NumNodes; i++ {
			res := <-ch
			if res.resp == nil {
				continue
			}
			if winner.resp == nil {
				winner = res // first success wins
			} else {
				res.resp.Body.Close() // discard extra responses
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
// It opens port 8080 immediately; proxyRequest handles node unavailability internally.
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
