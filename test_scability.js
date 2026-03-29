// Read performance test: 100x GET /customers
// Usage: node test_performance.js

const TARGET = "http://localhost:8080/customers";
const REQUESTS = 100;
const TIMEOUT_MS = 3000;

async function oneRequest() {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), TIMEOUT_MS);
  const started = Date.now();

  try {
    const res = await fetch(TARGET, { method: "GET", signal: ctl.signal });
    await res.text();
    clearTimeout(timer);
    return { ok: res.ok, ms: Date.now() - started };
  } catch {
    clearTimeout(timer);
    return { ok: false, ms: Date.now() - started };
  }
}

(async () => {
  console.log(`Target: ${TARGET}`);
  console.log(`Requests: ${REQUESTS}`);

  const warmup = await oneRequest();
  if (!warmup.ok) {
    console.error("Warm-up failed. Please start cluster first (gateway :8080).\nTry: ./start_cluster.sh 3");
    process.exit(1);
  }

  const latencies = [];
  let failed = 0;

  for (let i = 0; i < REQUESTS; i++) {
    const r = await oneRequest();
    if (r.ok) {
      latencies.push(r.ms);
    } else {
      failed++;
    }

    if ((i + 1) % 20 === 0) {
      const avgSoFar = latencies.length
        ? latencies.reduce((a, b) => a + b, 0) / latencies.length
        : 0;
      console.log(`[${i + 1}/${REQUESTS}] avg=${avgSoFar.toFixed(4)}ms failed=${failed}`);
    }
  }

  const success = latencies.length;
  const avg = success ? latencies.reduce((a, b) => a + b, 0) / success : 0;

  console.log("============================================================");
  console.log("Read Performance Result (GET /customers)");
  console.log(`Success: ${success}/${REQUESTS}`);
  console.log(`Failed: ${failed}`);
  console.log(`Average response time: ${avg.toFixed(4)}ms`);
  console.log("============================================================");
})();
