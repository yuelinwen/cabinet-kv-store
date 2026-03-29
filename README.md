# Cabinet KV Store

A distributed key-value store built on the [Cabinet weighted consensus protocol](https://arxiv.org/abs/2503.08914). Each node runs an independent Go process with its own MongoDB database, communicating via Go net/rpc over TCP.

## What is Cabinet?

Cabinet is a heterogeneous SMR (State Machine Replication) protocol where nodes are assigned priority weights based on their response speed. Faster nodes earn higher weights each round, allowing quorum to be reached without waiting for the slowest nodes.

Things not implemented:
- Log persistence / crash recovery
- Snapshotting and state transfer
- Dynamic cluster membership change
- Authentication

## Tech Stack

- **Language:** Go
- **Web framework:** Gin
- **Database:** MongoDB (per-node, `bank_db_0` / `bank_db_1` / `bank_db_2`)
- **Consensus:** Cabinet weighted SMR over Go net/rpc
- **Election:** Raft-style leader election with randomised timeout

## Architecture

```
Client (CLI / curl)
        │
        ▼
  Gateway :8080              ← HTTP proxy (net/http), forwards to current leader
        │
   ┌────┼────────────┐
   ▼    ▼            ▼
Node 0  Node 1    Node 2     ← each node is an independent Go process
:9080   :9081     :9082      ← Gin HTTP server (internal)
:9180   :9181     :9182      ← Cabinet RPC server (Go net/rpc over TCP)
  │       │         │
  ▼       ▼         ▼
MongoDB MongoDB  MongoDB     ← bank_db_0 / bank_db_1 / bank_db_2
```

- **GET** requests are served directly from the receiving node.
- **POST / PUT / DELETE** go through Cabinet consensus before being committed to all nodes.

## Running a Cluster

Requires MongoDB running locally (or Atlas URI configured in `server/database/`).

```bash
# Terminal 1 — Node 1 (follower)
cd server && go run . -id 1 -n 3

# Terminal 2 — Node 2 (follower)
cd server && go run . -id 2 -n 3

# Terminal 3 — Node 0 (initial leader + gateway)
cd server && go run . -id 0 -n 3 -gateway
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-id` | required | Node ID (`0` = initial leader) |
| `-n` | `3` | Total number of nodes in the cluster |
| `-t` | `floor((n-1)/2)` | Failure tolerance |
| `-gateway` | off |  Start the HTTP gateway/proxy on `:8080` |

### Example: 5-node cluster

```bash
go run . -id=0 -n=5 -gateway
go run . -id=1 -n=5
go run . -id=2 -n=5
go run . -id=3 -n=5
go run . -id=4 -n=5
```

### Example: 5-node cluster with custom tolerance (`-t 2`)

Allows up to 2 node failures in the worst case, up to 2 in the best case (n−t−1 = 5−2−1 = 2).

```bash
go run . -id=0 -n=5 -t=2 -gateway
go run . -id=1 -n=5 -t=2
go run . -id=2 -n=5 -t=2
go run . -id=3 -n=5 -t=2
go run . -id=4 -n=5 -t=2
```

## API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/customers` | List all customers |
| GET | `/customers/:id` | Get customer by ID |
| POST | `/customers` | Create customer |
| PUT | `/customers/:id` | Update customer |
| DELETE | `/customers/:id` | Delete customer |

```bash
# Create a customer
curl -X POST http://localhost:8080/customers \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "balance": 1000}'

# Get all customers
curl http://localhost:8080/customers
```

## Testing

### Functional Correctness Test (Node.js)

Tests the 5 core API routes end-to-end against a running cluster. Requires Node.js 18+.

**Start the cluster first** (see [Running a Cluster](#running-a-cluster)), then run:

```bash
node test_functional.js
```

The script runs 5 tests in sequence — each test depends on the previous one:

| # | Test | Method | Expected |
|---|------|--------|----------|
| 1 | GET all customers | `GET /customers` | 200 |
| 2 | Create new customer | `POST /customers` | 201 + auto-assigned UUID |
| 3 | Retrieve by ID | `GET /customers/:id` | 200 |
| 4 | Update customer | `PUT /customers/:id` | 200 |
| 5 | Delete customer | `DELETE /customers/:id` | 200 |

Expected output:

```
=====================================================================
  Cabinet KV Store — Functional Correctness Tests
  Target: http://localhost:8080
=====================================================================

  [PASS] GET /customers                    expected=200  actual=200  10 records returned
  [PASS] POST /customers                   expected=201  actual=201  id: "abc-123", name: "Test User"
  [PASS] GET /customers/:id                expected=200  actual=200  id: "abc-123", name: "Test User"
  [PASS] PUT /customers/:id                expected=200  actual=200  id: "abc-123", name: "Updated User"
  [PASS] DELETE /customers/:id             expected=200  actual=200

=====================================================================
  Results: 5/5 passed  (100% pass rate)
=====================================================================
```

### Scability Test (Node.js)

Runs 100 GET requests to `/customers` and reports average response time.

Using the helper scripts:

```bash
# One-time setup (WSL/Git Bash)
chmod +x start_cluster.sh stop_cluster.sh

# Start cluster with n nodes (default tolerance)
./start_cluster.sh 3

# Run scalability read test
node test_scability.js

# Stop all node processes
./stop_cluster.sh
```

Or if you start nodes manually, run:

```bash
node test_scability.js
```

What this test prints:

| Metric | Description |
|---|---|
| `Success` | Number of successful GET requests out of 100 |
| `Failed` | Number of failed/time-out requests |
| `Average response time` | Mean response latency in milliseconds |

Expected output shape:

```
Target: http://localhost:8080/customers
Requests: 100
[20/100] avg=...ms failed=...
[40/100] avg=...ms failed=...
...
============================================================
Read Performance Result (GET /customers)
Success: 100/100
Failed: 0
Average response time: 2.3456ms
============================================================
```

### Performance under Failures Test (Node.js)

Measures TPS and latency before and after a follower node is killed, across three kill strategies. Results are saved in [`failure_test_results.csv`](failure_test_results.csv).

**Start the cluster first, then run:**

```bash
node test_failure.js
```

The script runs 3 scenarios back-to-back. Between each scenario it will prompt you to **restart all 3 nodes** before continuing.

| Scenario | Killed node | What it tests |
|----------|-------------|---------------|
| Strong Kill | Node 1 (highest-weight follower) | Resilience when the fastest follower goes down |
| Weak Kill | Node 2 (lowest-weight follower) | Resilience when the slowest follower goes down |
| Random Kill | Node 1 or 2 (random) | Resilience under unpredictable failure |

Each scenario runs **12 rounds** of 3 concurrent POST requests. The target node is killed at **round 5**; rounds 6–12 observe recovery performance. Metrics recorded per round: TPS (writes/sec) and average latency (ms).

Test results are stored in **[`failure_test_results.csv`](failure_test_results.csv)**.

### Visual Test Page (Browser)

A browser-based test page is served by the gateway. With the cluster running:

```
http://localhost:8080/test
```

Click **▶ Run All Tests** to run all tests and see live pass/fail results.

## References

- [Cabinet: A Weighted Consensus Protocol (arXiv)](https://arxiv.org/abs/2503.08914)
