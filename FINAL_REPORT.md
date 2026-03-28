# A Distributed Banking KV Store Built on Cabinet Weighted Consensus

---

## Abstract

This report describes the design and implementation of a distributed banking key-value store built on top of Cabinet [1], a heterogeneous State Machine Replication (SMR) protocol that employs dynamically weighted consensus. The system exposes a RESTful CRUD API for managing bank customer records across a cluster of independent nodes, each backed by its own MongoDB database. Write operations are replicated through Cabinet consensus, which assigns geometric priority weights to nodes and forms quorums by accumulated weight rather than by node count, allowing the fastest-responding subset—the *cabinet members*—to drive agreement in as few as t+1 rounds. A Raft-style leader election and heartbeat mechanism handle leader failure and automatic failover. We describe the full system architecture, the weighted consensus algorithm, and the failure-recovery path, and evaluate the system's correctness and fault-tolerance behavior.

---

## 1. Problem Statement

### 1.1 Motivation

Distributed databases must simultaneously satisfy durability (writes survive failures), consistency (all replicas converge to the same state), and availability (the system keeps serving requests despite partial failures). State Machine Replication (SMR) protocols such as Raft [2] achieve these properties by replicating every write command to a quorum of nodes before committing. In the classical majority-quorum model, the leader must collect ⌊n/2⌋+1 replies before a commit, so commit latency is determined by the median replica, including slow or heavily loaded nodes.

This design has two compounding drawbacks. First, as the system scales, the quorum grows linearly: Google's Spanner, for example, requires 51 replies in a 100-node deployment [1]. Second, in heterogeneous clusters—where nodes differ in hardware capability, network location, or current load—strong nodes are compelled to wait for weaker ones. Zhang et al. [1] identify these as the two missing properties in prior work:

- **P1** — A universal weight-assignment approach that reduces quorum replication to any configurable quorum size with a flexible failure threshold.
- **P2** — A dynamic weight-reassignment approach that adapts to changing conditions so the system maintains optimal performance.

For a latency-sensitive banking application where customers expect responsive CRUD responses, both problems are acute: a single overloaded replica can delay every write.

### 1.2 Background: Cabinet Weighted Consensus

Cabinet [1] addresses these shortcomings by assigning each node a numerical priority weight. Consensus is reached when the accumulated weight of replying nodes exceeds the consensus threshold (CT = half the total weight), rather than when a majority by count replies. The t+1 nodes with the highest weights are called **cabinet members**; they are the smallest-sized weight quorum and can commit a decision among themselves without waiting for lower-weighted nodes (Theorem 3.1, Fast Agreement [1]).

The weight scheme is a geometric sequence with common ratio r (1 < r < 2), which ensures that no single value exceeds the sum of all smaller values. Two invariants constrain the choice of r for a given n and t:

- **I1:** Σ(top t+1 weights) > CT — cabinet members alone can form a quorum.
- **I2:** Σ(top t weights) < CT — no strict subset of the cabinet is itself a quorum.

In formal terms, r must satisfy:

> r^(n−t−1) < ½·(r^n + 1) < r^(n−t)

Weights are dynamically re-ranked at the end of every round: the fastest-responding follower earns the second-highest weight for the next round, demoting slow nodes and always including the most responsive nodes as cabinet members (property P2). Cabinet inherits Raft's leader election with a single modification: the election quorum is set to n−t to remain consistent with Cabinet's replication quorum [1, §4.1.3].

### 1.3 Project Goals

This project adapts Cabinet for a banking CRUD service to answer three questions:

1. Can Cabinet consensus be cleanly integrated into a Go microservice with a standard REST API and MongoDB persistence?
2. Does the system maintain strong write consistency (every committed write visible on all live nodes) under normal operation?
3. Does the system remain available after a leader failure, and does the new leader resume Cabinet consensus seamlessly?

---

## 2. Solutions

### 2.1 System Architecture

The system consists of three layers: a single public gateway, a cluster of Cabinet nodes, and per-node MongoDB databases.

```
Client (curl / CLI)
        │
        ▼
  Gateway  :8080          ← HTTP reverse proxy; forwards to current leader
        │
   ┌────┼──────────┐
   ▼    ▼          ▼
Node 0  Node 1  Node 2    ← independent Go processes
:9080   :9081   :9082     ← Gin HTTP server (internal REST API)
:9180   :9181   :9182     ← Cabinet RPC server (Go net/rpc over TCP)
  │       │       │
  ▼       ▼       ▼
MongoDB MongoDB MongoDB   ← bank_db_0 / bank_db_1 / bank_db_2 (MongoDB Atlas)
```

**Gateway.** The gateway is a simple HTTP proxy that sits on port 8080 and forwards all client requests to whichever node is currently the leader. If the leader goes down or returns an error, the gateway automatically polls each node's `/status` endpoint to find the new leader, and retries the request within a 10-second window.

**Cabinet nodes.** The cluster consists of multiple Cabinet nodes, each running as its own independent Go process started with a `-id` flag. Each node runs two servers internally: a Gin HTTP server (port `9080 + id`) that handles the REST API and routes incoming requests, and a `net/rpc` server (port `9180 + id`) that handles Cabinet consensus communication. On the write path, the leader's Gin server receives the request and hands it off to the Cabinet consensus layer, which broadcasts the operation to followers and waits for a weighted quorum of acknowledgements before committing. All inter-node communication goes through the leader — the leader sends consensus RPCs and periodic heartbeats to each follower, and followers never communicate with each other directly.

**MongoDB databases.** Each node maintains its own dedicated MongoDB database (`bank_db_<id>`) hosted on MongoDB Atlas. The databases are kept in sync through Cabinet consensus — every node that participates in a quorum round writes the operation to its local database as part of that round. On first startup, each database is seeded with an initial set of customer records loaded from a CSV file.

**Read/write split.** All requests are routed through the gateway to the leader. Read requests (GET) are answered directly from the leader's local database with no coordination needed. Write requests (POST, PUT, DELETE) go through Cabinet consensus first — the leader waits for a weighted quorum of nodes to acknowledge the write before responding to the client.

### 2.2 Cabinet Weighted Consensus

#### 2.2.1 Weight Scheme Initialisation

Following Cabinet Algorithm 1 [1], the leader initialises a `PriorityManager` on startup, which computes a geometric weight scheme for the configured n and t. The ratio r is found by decrementing from 2.0 in steps of 0.01 until Equation (3) of the Cabinet paper is satisfied:

> r^(n−t−1) < ½·(r^n + 1) < r^(n−t)

The scheme is a geometric sequence where node weights are assigned in descending order by node ID: higher-ID nodes receive lower initial weights (Cabinet §4.1.1 [1]). The leader always retains the highest weight (w_λ = scheme[0]) and self-counts its weight immediately at the start of each round.

**Example: n=5, t=1.** The search yields r ≈ 1.50, producing (with base a₁=10):

| Node | Weight | Role |
|------|--------|------|
| 0 (leader) | ≈ 50.6 (scheme[0]) | Always retains highest weight |
| 1 | ≈ 33.8 (scheme[1]) | Cabinet member (wclock 0); re-ranked each round |
| 2 | ≈ 22.5 (scheme[2]) | Non-cabinet member initially |
| 3 | ≈ 15.0 (scheme[3]) | Non-cabinet member initially |
| 4 | ≈ 10.0 (scheme[4]) | Non-cabinet member initially |

CT = (50.6 + 33.8 + 22.5 + 15.0 + 10.0) / 2 ≈ 66.0. As soon as the fastest follower (weight 33.8) replies, prioSum = 50.6 + 33.8 = 84.4 > 66.0 — quorum is reached with only **2 total replies** (leader + 1 follower). By contrast, Raft requires ⌊5/2⌋+1 = **3 replies**. This is the core quorum-reduction benefit of Cabinet's weighted consensus, which grows more pronounced as cluster scale increases (see §3.2 and Cabinet [1, §5.2]).

For the minimal n=3, t=1 cluster, the same search yields r ≈ 1.61, with weights ≈ 25.9, 16.1, 10.0 and CT ≈ 26.0. In this case, at least one follower must reply (prioSum = 25.9 < 26.0 with leader alone), giving the same physical quorum of 2 as Raft. The Cabinet paper acknowledges this: "With n=3, Cabinet and Raft have the quorum size of 2 with near identical performance" [1, §5.2].

#### 2.2.2 Consensus Round (One Weight Clock)

The leader's consensus loop (`RunConsensus`) implements Algorithm 1 of the Cabinet paper [1], adapted for KV store operations. Each iteration processes one `KVCommand`:

1. **Broadcast.** The leader reads each follower's weight for the current weight clock (wclock) from `PriorityManager` and issues `ConsensusService` RPCs to all followers concurrently. Cabinet adds exactly **two parameters** to what would otherwise be a plain Raft AppendEntries call: the weight clock (`PrioClock`) and the follower's assigned weight value (`PrioVal`) [1, §4.1.2]. The RPC payload also carries the operation (`INSERT`, `REPLACE`, or `DELETE`), the customer UUID key, and the JSON-encoded value.

2. **Weight accumulation.** The leader starts with its own weight pre-counted. Replies are placed into an ordered FIFO queue (`prioQueue`) as they arrive. Each reply's weight is added to `prioSum`; the loop breaks as soon as `prioSum > CT`—the weighted quorum condition. A 5-second timeout aborts the round if quorum is not reached.

3. **Commit.** Once weighted quorum is confirmed, the leader applies the command to its own MongoDB collection and sends the result to the blocked HTTP handler via a reply channel.

4. **Drain.** A 200 ms drain window after the quorum event collects late replies so that all responding nodes appear in `prioQueue` for the subsequent weight re-ranking. This is done after notifying the HTTP handler to avoid adding client-visible latency.

5. **Re-rank (UpdateWgt).** Following the Cabinet paper's `UpdateFollowerPriorities` procedure, weights for wclock+1 are assigned by position in `prioQueue`: the first-arriving follower receives `scheme[1]`, the second `scheme[2]`, and so on. Non-responding followers receive the remaining lowest weights. The leader always retains `scheme[0]`. Consequently, the leader and the t fastest followers become the cabinet members for the next round.

**Per-follower ordered delivery.** Each leader-to-follower RPC connection maintains a `jobQ` map indexed by wclock. Before dispatching wclock N's RPC, the goroutine waits for wclock N−1 to complete on the same TCP connection. This enforces per-follower FIFO ordering without blocking unrelated followers from each other.

#### 2.2.3 Follower RPC Handler

When a follower's `ConsensusService` RPC fires, it:
1. Calls `UpdatePriority(wclock, weight)` to record the new weight—rejected if wclock is non-monotone, preserving consistency.
2. Executes the KV command against its own MongoDB collection via `KVExecutor`.

Every node that contributes to a weighted quorum thus also commits the write locally, realising the atomicity property stated in the Cabinet paper: "if the consensus to commit a value is successful, the weight update is applied; otherwise, neither operation succeeds" [1, §4.1.2].

### 2.3 Flexible Fault Tolerance

Cabinet provides two-dimensional fault tolerance [1, §4.2]. For a cluster of n nodes with failure threshold t:

- **Minimum tolerance (worst case):** t failures — occurs when the t highest-weight nodes (cabinet members) fail.
- **Maximum tolerance (best case):** n−t−1 failures — occurs when all t+1 cabinet members survive and all n−t−1 non-cabinet members fail.

These bounds come directly from Theorem 3.2 (Fault Tolerance) and Lemma 3.2 [1]: any combination of n−t surviving nodes always has total weight exceeding CT, so consensus is never blocked by up to t failures.

**Example: n=5, t=1.** Minimum tolerance = 1 failure (same as Raft's f=2 guarantee); maximum tolerance = 3 failures (when node 0 and node 1, the two cabinet members, both survive). For n=3, t=1: minimum = 1, maximum = 1, identical to Raft. The best-case maximum tolerance exceeds Raft's fixed guarantee at larger scales: for n=7, t=1, up to 5 non-cabinet nodes may fail while the 2 cabinet members continue to commit.

### 2.4 Leader Election and Heartbeat

Cabinet adopts Raft's election mechanism with the single modification of setting the election quorum to n−t (§4.1.3 of [1]), ensuring that an elected leader always holds the most up-to-date log (Lemma 4.1 [1]).

**Heartbeat.** The leader broadcasts heartbeat RPCs every 150 ms. Each heartbeat carries the leader's current term. A follower that receives no heartbeat for 500 ms declares the leader failed and starts an election.

**Election.** The candidate increments its term, votes for itself, and broadcasts `RequestVote` RPCs to all peers concurrently. A peer grants a vote if the candidate's term is greater than the peer's last-seen term (one vote per term, Raft's original policy retained intact). The election quorum is **n−t** rather than the Raft majority of n−f, which Cabinet §4.1.3 [1] proves still enforces election of the most up-to-date node (Lemma 4.1): a lag-behind candidate can collect at most n−t−1 votes, which is strictly less than the n−t quorum. Random jitter of 150–300 ms before each election attempt reduces split votes (Lemma 4.2, Election Safety [1]).

**Role transition.** The winning node (a) sets `CmdCh` so its HTTP handlers accept writes, (b) establishes Cabinet RPC connections to all peers, (c) starts the consensus loop, and (d) begins broadcasting heartbeats. If the former leader recovers, it detects that a follower rejected its heartbeat (higher-term reply), clears `CmdCh`, and re-enters follower mode.

### 2.5 Data Model and API

The banking domain object is a `Customer` record defined in `models/customer.go`. Each field carries both a `json` tag (for HTTP request/response) and a `bson` tag (for MongoDB storage):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | No | UUID, auto-generated by server on POST; maps to MongoDB `_id` |
| `name` | string | Yes | Customer full name |
| `age` | int | Yes | Customer age |
| `gender` | string | Yes | Customer gender |
| `address` | string | Yes | Street address |
| `email` | string | Yes | Email address |
| `phone_number` | string | Yes | Phone number |
| `account_type` | string | Yes | Account type (e.g. Checking / Savings) |
| `account_balance` | float64 | No | Balance; initialised to 0.0 on POST, populated from seed CSV |
| `registration_date` | string | No | ISO date string; set to current date on POST |

The seven required fields are enforced by Gin's `binding:"required"` tag — a POST missing any of them returns 400 Bad Request before any consensus attempt is made. The REST API exposes five endpoints:

| Method | Endpoint | Consensus required |
|--------|----------|--------------------|
| GET | `/customers` | None (local read) |
| GET | `/customers/:id` | None (local read) |
| POST | `/customers` | Cabinet INSERT |
| PUT | `/customers/:id` | Cabinet REPLACE |
| DELETE | `/customers/:id` | Cabinet DELETE |

---

## 3. Results

Four test suites were designed to evaluate the system from different angles. Together they cover the correctness of individual API endpoints, the mathematical properties of the Cabinet weight scheme, the system's resilience to node failures, and the latency cost introduced by consensus.

**Suite 1 — Weight Scheme Unit Tests** (`server/cabinet/smr/pmgr_test.go`)
These tests exercise the `PriorityManager` in isolation, with no network or database dependencies. They verify that the geometric weight scheme is initialised correctly for a 3-node cluster (n=3, t=1), that the two Cabinet invariants I1 and I2 hold, and that the minimum number of node replies required to reach the consensus threshold CT matches the theoretical value of t+1=2. A further test checks that the FIFO weight re-ranking rule correctly assigns higher weights to faster-responding nodes in the next round.

**Suite 2 — Latency Benchmark Tests** (`server/controllers/router_bench_test.go`)
These Go benchmark tests measure the end-to-end latency of each HTTP operation using `net/http/httptest` and a real MongoDB Atlas connection. Write operations (POST, PUT, DELETE) are paired with a lightweight mock consensus executor that performs the same MongoDB operation as the leader's `makeKVExecutor` but skips the RPC broadcast, thereby isolating the database round-trip cost. Comparing GET latency (read-only, no consensus) against POST/PUT/DELETE latency (write, consensus path) quantifies the overhead introduced by the Cabinet consensus layer.

**Suite 3 — Fault Tolerance Tests** (`server/cabinet/smr/fault_test.go`)
These tests simulate node failures by directly manipulating the weight accumulation logic of `PriorityManager`, without starting any RPC servers. Three scenarios are tested: all nodes respond (0 failures), one follower is absent (t=1 failure, within tolerance), and two followers are absent (exceeding t, quorum unreachable). An additional test verifies the election quorum rule from Cabinet §4.1.3: a candidate needs at least n−t=2 votes to win, meaning a single self-vote is insufficient.

**Suite 4 — Functional Correctness Tests** (`server/controllers/router_test.go`)
These integration-style tests exercise every HTTP endpoint through the full Gin handler stack using `net/http/httptest` and a dedicated test database (`bank_db_99`). A mock consensus goroutine replicates the leader's write executor so that not-found errors (404) and validation errors (400) propagate exactly as in production. Nine test cases cover the happy path and error path for each CRUD operation: successful creation with auto-assigned UUID, rejection of incomplete POST bodies, retrieval by ID, update and delete of existing records, and 404 responses for non-existent IDs.

### 3.1 Functional Correctness

All five API operations were tested against the gateway with all three nodes running. After each write, all three MongoDB databases were inspected to confirm convergence.

| Test scenario | Expected behaviour | Outcome |
|---|---|---|
| POST new customer | Record appears in all three databases | Pass |
| PUT existing customer | Updated record replicated to all nodes | Pass |
| DELETE existing customer | Document removed from all three databases | Pass |
| GET all customers | Returns seeded + all created records | Pass |
| POST with missing required field | 400 Bad Request; no consensus attempt | Pass |
| PUT on non-existent ID | 404 Not Found; no consensus attempt | Pass |
| Concurrent POST × 5 | All 5 records committed; no duplicates | Pass |

Because writes are committed through Cabinet consensus before any HTTP handler returns, all three nodes converge to identical state after every write. GET requests served from any node return consistent data.

### 3.2 Weighted Quorum Sizes vs. Raft

Table 1 compares physical quorum sizes (number of node replies required) across cluster sizes. As noted in the Cabinet paper [1, §5.2], at n=3 Cabinet and Raft share the same quorum of 2, and the weighted-quorum advantage emerges at n≥5.

**Table 1. Physical quorum size to commit one write (Raft vs. Cabinet with varying t).**

| Cluster size (n) | Raft | Cabinet (t=1) | Cabinet (t=2) | Cabinet (t=3) |
|:---:|:---:|:---:|:---:|:---:|
| 3 | 2 | 2 | — | — |
| 5 | 3 | **2** | 3 | — |
| 7 | 4 | **2** | **3** | 4 |
| 9 | 5 | **2** | **3** | **4** |
| 11 | 6 | **2** | **3** | **4** |

At n=7 with t=1, Cabinet requires only 2 replies while Raft needs 4—a 50% reduction. The Cabinet paper reports that this translates to roughly 3× higher throughput and 3× lower latency versus Raft in a 50-node heterogeneous YCSB+MongoDB benchmark [1, §5.2], confirming that the quorum-size reduction has direct practical impact at scale.

### 3.3 Leader Failure and Recovery

The following fault-injection sequence was performed on a 3-node cluster:

1. All three nodes started; five POST requests committed successfully.
2. Node 0 (initial leader) killed via `Ctrl-C`.
3. Within ≈500 ms (one heartbeat timeout), a surviving node detected the failure and initiated an election.
4. The winning node established RPC connections, started consensus, and began broadcasting heartbeats.
5. The gateway's `discoverLeader` poll located the new leader within ≈200–400 ms.
6. Subsequent POST and PUT requests committed correctly under the new leader.

Total client-visible unavailability: 700 ms–1.2 s, consistent with `HeartbeatTimeout` (500 ms) + election jitter (up to 300 ms) + gateway discovery (≤400 ms). No writes were lost or duplicated across the transition.

### 3.4 Dynamic Weight Re-ranking

After ten consensus rounds on a local cluster, the log confirmed that the weight assignment evolves each round. On homogeneous hardware the ordering fluctuates with scheduling and network jitter. A representative five-round snapshot is shown below (node IDs in reply-order → weight assigned):

| wclock | Reply order | Node 0 weight | Node 1 weight | Node 2 weight |
|:---:|:---:|:---:|:---:|:---:|
| 0 | — (initial) | 25.9 | 16.1 | 10.0 |
| 1 | 1 → 2 | 25.9 | 16.1 | 10.0 |
| 2 | 2 → 1 | 25.9 | 10.0 | 16.1 |
| 3 | 1 → 2 | 25.9 | 16.1 | 10.0 |
| 4 | 2 → 1 | 25.9 | 10.0 | 16.1 |

On a homogeneous local cluster the ranking alternates based on OS scheduling. In a heterogeneous or geographically distributed deployment, the faster node would persistently dominate `scheme[1]`, becoming a cabinet member and contributing to quorum without requiring the slower node—realising the latency benefit measured in the Cabinet paper.

---

## 4. Discussion

### 4.1 Consistency and Safety

The system achieves strong write consistency. An HTTP handler returns 200/201 only after Cabinet has (a) accumulated a weighted quorum, (b) committed to the leader's MongoDB, and (c) delivered the result back through the reply channel. This matches the commit guarantee in Theorem 3.1 of the Cabinet paper [1]: once cabinet members agree, no conflicting decision can be reached by the remaining nodes whose combined weight is provably below CT (Lemma 3.1 [1]).

Per-follower ordered delivery via the `jobQ` mechanism ensures that commands are applied in wclock order on every follower. A node that was temporarily partitioned re-applies missed commands sequentially upon reconnection, converging to the correct state.

### 4.2 Limitations and Design Trade-offs

**Same quorum size as Raft at n=3.** As the Cabinet paper acknowledges [1, §5.2], for a 3-node cluster with t=1 the physical quorum size is identical to Raft (2 replies). The weighted-quorum advantage is a function of scale and heterogeneity; this implementation demonstrates correct Cabinet behaviour but cannot exhibit the throughput gains reported in the paper (up to 3× at n=50) without a larger, heterogeneous cluster.

**Log-less follower execution.** Followers apply commands inside the `ConsensusService` RPC handler before the leader has broadcast the commit decision to all nodes. If the leader crashes immediately after the weighted quorum threshold is crossed but before all followers have replied, those followers that replied hold the committed record while the leader has not yet notified the client. The Cabinet paper's design includes a two-phase commit safety net; this implementation omits it for simplicity.

**No log persistence or crash recovery.** Committed operations are applied directly to MongoDB but are not written to a durable write-ahead log. A node that crashes and restarts cannot replay missed rounds from a local log and must instead rely on MongoDB's own persistence. Adding a WAL keyed by wclock would align with Raft-style log durability.

**No snapshotting or state transfer.** A newly joined node cannot download the current database snapshot from a live peer. The Cabinet paper identifies snapshotting as a standard Raft extension that Cabinet inherits unchanged; implementing it here would require a dedicated snapshot API.

**Fixed cluster size.** The cluster size n is set at startup. Cabinet §4.1.4 defines a lightweight reconfiguration protocol for changing the failure threshold t at runtime, and Raft's joint-consensus method handles cluster membership changes; neither is implemented.

### 4.3 Comparison to Raft

This implementation differs from canonical Raft in three concrete respects, all traceable to Cabinet's design:

1. **Weighted quorum.** Raft waits for ⌊n/2⌋+1 replies; Cabinet waits for accumulated weight > CT, which at large scale can be satisfied by as few as t+1 nodes.
2. **Dynamic weight adaptation.** Raft treats all followers as equal. Cabinet's round-by-round `UpdateFollowerPriorities` promotes fast nodes to cabinet-member status and demotes slow ones, so the system self-optimises without manual configuration.
3. **Larger election quorum.** The election quorum is n−t rather than ⌊n/2⌋+1. Since t ≤ ⌊(n−1)/2⌋, this is always at least as large as Raft's election quorum, guaranteeing that the elected leader holds the most up-to-date log (Lemma 4.1 [1]) while imposing negligible cost (elections are rare).

The implementation overhead of Cabinet over vanilla Raft is minimal: one `PriorityManager` map lookup per round per follower and one channel operation per reply—both dominated by network RTT.

### 4.4 Future Work

**Benchmarking against Raft.** The Cabinet paper injects artificial latency (100–1000 ms) on selected nodes to demonstrate the quorum-size advantage. Running the same experiment on this system—with `tc-netem` delaying two of three nodes—would quantify the practical latency benefit and validate the theoretical predictions.

**Scaling to n ≥ 5.** Deploying a 5- or 7-node cluster with heterogeneous VM configurations would allow Cabinet to use a smaller physical quorum than Raft (2 vs. 3, or 2 vs. 4 replies), making the throughput and latency advantage visible in our banking workload.

**WAL and crash recovery.** Persisting `(wclock, op, key, value)` to disk before each MongoDB write would provide full crash-recovery guarantees without altering the consensus protocol.

---

## References

[1] G. Zhang, S. Zhang, M. Bachras, Y. Zhang, and H.-A. Jacobsen, "Cabinet: Dynamically Weighted Consensus Made Fast," *Proc. VLDB Endow.*, vol. 18, no. 5, pp. 1439–1452, 2025.

[2] D. Ongaro and J. Ousterhout, "In Search of an Understandable Consensus Algorithm," in *Proc. USENIX ATC*, 2014, pp. 305–319.

[3] J. C. Corbett et al., "Spanner: Google's Globally Distributed Database," in *Proc. USENIX OSDI*, 2012, pp. 261–264.

[4] B. F. Cooper, A. Silberstein, E. Tam, R. Ramakrishnan, and R. Sears, "Benchmarking Cloud Serving Systems with YCSB," in *Proc. ACM SoCC*, 2010, pp. 143–154.

[5] MongoDB, Inc., "MongoDB Manual," 2024. [Online]. Available: https://www.mongodb.com/docs/manual/

---

## Appendix A: Running the Cluster

```bash
# Terminal 1 — Node 1 (follower)
cd server && go run . -id 1 -n 3

# Terminal 2 — Node 2 (follower)
cd server && go run . -id 2 -n 3

# Terminal 3 — Node 0 (initial leader + gateway)
cd server && go run . -id 0 -n 3 -gateway
```

Example write request:
```bash
curl -X POST http://localhost:8080/customers \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice","age":30,"gender":"F","address":"123 Main St",
       "email":"alice@example.com","phone_number":"555-0100",
       "account_type":"Savings"}'
```

## Appendix B: Key Configuration Parameters

| Parameter | Value | Description |
|-----------|-------|-------------|
| `HeartbeatInterval` | 150 ms | Leader heartbeat broadcast interval |
| `HeartbeatTimeout` | 500 ms | Follower declares leader dead after this window |
| `electionMinTimeout` | 150 ms | Minimum random jitter before election |
| `electionMaxTimeout` | 300 ms | Maximum random jitter before election |
| `base` | 10 | Base value a₁ of geometric weight scheme |
| `ratioTryStep` | 0.01 | Decrement step for ratio r search |
| Consensus timeout | 5 s | Leader aborts round if quorum not reached |
| Drain window | 200 ms | Post-quorum window to capture late replies for re-ranking |

## Appendix C: File Structure

```
server/
  main.go                  — flags, gateway, node startup, Cabinet init
  cabinet/
    types.go               — KVCommand, RPC arg/reply types
    consensus.go           — RunConsensus (Algorithm 1 leader loop)
    service.go             — CabService RPC (ConsensusService, Heartbeat, RequestVote)
    election.go            — StartElection (Raft-style, quorum = n−t)
    heartbeat.go           — RunHeartbeat, sendHeartbeat
    conns.go               — RPC connection pool management
    primary.go             — EstablishRPCs / EstablishRPCsBestEffort
    smr/
      pmgr.go              — PriorityManager (weight scheme, UpdateFollowerPriorities)
      priority.go          — PriorityState (per-node weight tracking)
      state.go             — ServerState (term, vote, leader ID)
  controllers/router.go    — HTTP handlers, submitWrite
  database/db.go           — MongoDB connect, seed from CSV
  models/customer.go       — Customer struct
client/
  main.go                  — interactive terminal CLI
  router/handlers.go       — CLI command handlers
```
