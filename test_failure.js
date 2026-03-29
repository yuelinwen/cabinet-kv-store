/**
 * Cabinet KV Store — §5.4 Failure Performance Test
 *
 * 3 scenarios (restart cluster between each):
 *   1. Strong Kill — Node 1 (w2, cabinet member) killed at Round 20
 *   2. Weak Kill   — Node 2 (w3, non-cabinet)    killed at Round 30
 *   3. Random Kill — random follower              killed at Round 40
 *
 * Metrics recorded per round:
 *   TPS     — successful writes / elapsed seconds
 *   Latency — avg ms per successful POST (client round-trip through Cabinet consensus)
 *
 * Usage: node test_failure.js
 */

"use strict";
const { exec } = require("child_process");
const readline = require("readline");

// ── Config ─────────────────────────────────────────────────────────────────────
const BASE          = "http://localhost:8080";
const ROUNDS        = 12;   // rounds per scenario
const OPS_PER_ROUND = 3;    // concurrent POSTs per round
const ROUND_GAP_MS  = 300;  // pause between rounds
const OP_TIMEOUT_MS = 6000; // per-request abort timeout

// Internal Gin ports — node 0 (leader, :9080) is never killed
const FOLLOWER_PORT = { 1: 9081, 2: 9082 };

// ── ANSI ───────────────────────────────────────────────────────────────────────
const G = "\x1b[92m", R = "\x1b[91m", Y = "\x1b[93m", C = "\x1b[96m", W = "\x1b[97m", X = "\x1b[0m";

// ── Helpers ────────────────────────────────────────────────────────────────────
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function waitForEnter(prompt = "  Press Enter to continue...") {
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  return new Promise((resolve) => rl.question(prompt, () => { rl.close(); resolve(); }));
}

function ts() {
  return new Date().toISOString().replace("T", " ").slice(0, 19);
}

// Kill all LISTENING processes on the given TCP port (Windows taskkill).
function killPort(port) {
  return new Promise((resolve) => {
    exec("netstat -ano", (err, stdout) => {
      if (err || !stdout) return resolve(false);
      const pids = new Set();
      for (const line of stdout.split("\n")) {
        const p = line.trim().split(/\s+/);
        if (p.length >= 5 && p[3] === "LISTENING") {
          const col = p[1].lastIndexOf(":");
          if (col !== -1 && p[1].slice(col + 1) === String(port)) pids.add(p[4]);
        }
      }
      if (!pids.size) return resolve(false);
      let done = 0;
      for (const pid of pids)
        exec(`taskkill /PID ${pid} /F`, () => { if (++done === pids.size) resolve(true); });
    });
  });
}

// ── Write operation (returns { ok, latencyMs }) ────────────────────────────────
let seq = 0;
async function sendWrite() {
  const n = ++seq;
  const body = JSON.stringify({
    name: `FailTest_${n}`, age: 20 + (n % 60),
    gender: n % 2 === 0 ? "Male" : "Female",
    address: `${n} Cabinet Ave`,
    email: `t${n}@cab.test`,
    phone_number: `555-${String(n).padStart(4, "0")}`,
    account_type: n % 2 === 0 ? "Checking" : "Savings",
  });
  const t0 = Date.now();
  try {
    const res = await fetch(BASE + "/customers", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body,
      signal: AbortSignal.timeout(OP_TIMEOUT_MS),
    });
    const latencyMs = Date.now() - t0;
    return { ok: res.status === 201, latencyMs };
  } catch {
    return { ok: false, latencyMs: Date.now() - t0 };
  }
}

// ── Run one scenario ───────────────────────────────────────────────────────────
// killRounds: array of round numbers at which to attempt killing the target node.
// The first attempt terminates the process; subsequent attempts are no-ops
// (node already dead) and are logged as [DEAD] for reference.
async function runScenario(scenarioName, killRounds, targetNode) {
  const port = FOLLOWER_PORT[targetNode];
  const killSet = new Set(killRounds);

  console.log(`\n${C}${"═".repeat(68)}${X}`);
  console.log(`${C}  SCENARIO : ${W}${scenarioName}${X}`);
  console.log(`${C}  Kill     : Node ${targetNode} (port ${port}) at rounds ${killRounds.join(", ")}${X}`);
  console.log(`${C}  Start    : ${ts()}${X}`);
  console.log(`${C}${"═".repeat(68)}${X}`);
  console.log(`\n  ${"Rnd".padStart(4)}  ${"TPS".padStart(8)}  ${"Avg Lat".padStart(9)}  ${"OK".padStart(4)}/${"N".padEnd(3)}  ${"Fail".padStart(4)}  Event`);
  console.log(`  ${"-".repeat(56)}`);

  const rows = [];
  let wasKilled = false;

  for (let round = 1; round <= ROUNDS; round++) {

    // ── Kill ───────────────────────────────────────────────────────────────
    if (killSet.has(round)) {
      if (!wasKilled) {
        console.log(`\n  ${Y}>>> Round ${round}: killing Node ${targetNode} (port ${port}) <<<${X}`);
        const ok = await killPort(port);
        if (ok) {
          console.log(`  ${G}  taskkill succeeded — port ${port} process terminated.${X}`);
        } else {
          console.log(`  ${R}  Auto-kill failed. Stop Node ${targetNode} manually (-id ${targetNode}), then press Enter.${X}`);
          await waitForEnter("  Enter: ");
        }
        wasKilled = true;
        await sleep(400);
        console.log();
      } else {
        console.log(`\n  ${Y}>>> Round ${round}: Node ${targetNode} already dead — skipping kill <<<${X}\n`);
      }
    }

    // ── Batch ──────────────────────────────────────────────────────────────
    const t0      = Date.now();
    const results = await Promise.allSettled(
      Array.from({ length: OPS_PER_ROUND }, () => sendWrite())
    );
    const elapsedSec = Math.max(Date.now() - t0, 1) / 1000;

    const successful = results
      .filter((r) => r.status === "fulfilled" && r.value.ok)
      .map((r) => r.value.latencyMs);

    const okCount    = successful.length;
    const failCount  = OPS_PER_ROUND - okCount;
    const tps        = okCount / elapsedSec;
    const avgLat     = okCount > 0
      ? (successful.reduce((s, v) => s + v, 0) / okCount).toFixed(1)
      : "—";

    // event label: KILL on the first kill round, DEAD on all subsequent ones
    let event = "";
    if (killSet.has(round)) event = round === killRounds[0] ? "KILL" : "DEAD";
    rows.push({ round, tps, avgLatMs: okCount > 0 ? parseFloat(avgLat) : null, okCount, failCount, event });

    // ── Print ──────────────────────────────────────────────────────────────
    let eventStr;
    if (event === "KILL")                  eventStr = `${Y}[KILL]${X}`;
    else if (event === "DEAD")             eventStr = `${Y}[DEAD]${X}`;
    else if (failCount === OPS_PER_ROUND)  eventStr = `${R}ALL FAILED${X}`;
    else if (failCount > 0)                eventStr = `${Y}DEGRADED${X}`;
    else                                   eventStr = `${G}OK${X}`;

    console.log(
      `  ${String(round).padStart(4)}` +
      `  ${tps.toFixed(1).padStart(8)}` +
      `  ${String(avgLat).padStart(8)}ms` +
      `  ${String(okCount).padStart(4)}/${String(OPS_PER_ROUND).padEnd(3)}` +
      `  ${String(failCount).padStart(4)}  ${eventStr}`
    );

    await sleep(ROUND_GAP_MS);
  }

  console.log(`\n  ${C}Scenario done at ${ts()}${X}`);
  return rows;
}

// ── Summary ────────────────────────────────────────────────────────────────────
function printSummary(scenarios) {
  const div = "═".repeat(68);
  console.log(`\n${div}`);
  console.log(`  SUMMARY — Cabinet §5.4 Failure Test   ${ts()}`);
  console.log(`  n=3 t=1 | ${ROUNDS} rounds/scenario | ${OPS_PER_ROUND} ops/round`);
  console.log(div);

  for (const { name, killRounds, rows } of scenarios) {
    // firstKill = first round where the kill actually happens
    const firstKill = killRounds[0];
    const pre  = rows.filter((r) => r.round < firstKill);
    const post = rows.filter((r) => r.round > firstKill);

    const avg = (arr, key) =>
      arr.length ? (arr.reduce((s, r) => s + (r[key] ?? 0), 0) / arr.length).toFixed(1) : "N/A";

    const preTPS  = avg(pre,  "tps");
    const postTPS = avg(post, "tps");
    const preLat  = avg(pre.filter(r => r.avgLatMs !== null),  "avgLatMs");
    const postLat = avg(post.filter(r => r.avgLatMs !== null), "avgLatMs");
    const delta   = preTPS !== "N/A" && postTPS !== "N/A"
      ? ((parseFloat(postTPS) - parseFloat(preTPS)) / parseFloat(preTPS) * 100).toFixed(1)
      : "N/A";
    const arrow   = parseFloat(delta) >= 0 ? "+" : "";
    const totalOk = rows.reduce((s, r) => s + r.okCount, 0);
    const totalOp = rows.length * OPS_PER_ROUND;

    console.log(`\n  ┌─ ${W}${name}${X}  (kill at rounds ${killRounds.join(", ")})`);
    console.log(`  │`);
    console.log(`  │  Avg TPS      before kill : ${preTPS.padStart(8)} TPS`);
    console.log(`  │  Avg TPS      after  kill : ${postTPS.padStart(8)} TPS   (${arrow}${delta}%)`);
    console.log(`  │  Avg Latency  before kill : ${preLat.padStart(8)} ms`);
    console.log(`  │  Avg Latency  after  kill : ${postLat.padStart(8)} ms`);
    console.log(`  │  Success rate            : ${totalOk}/${totalOp} = ${(totalOk / totalOp * 100).toFixed(1)}%`);
    console.log(`  └${"─".repeat(54)}`);
  }

  // ── Side-by-side per-round table ──────────────────────────────────────────
  const w = 20;
  const hdr = scenarios.map((s) => s.name.padEnd(w)).join(" ");
  console.log(`\n\n  Per-round log (TPS / Lat ms)   * = first kill   ~ = node already dead`);
  console.log(`  ${"Rnd".padStart(4)}  ${hdr}`);
  console.log(`  ${"-".repeat(4 + 2 + scenarios.length * (w + 1))}`);

  for (let r = 1; r <= ROUNDS; r++) {
    const cols = scenarios.map(({ rows }) => {
      const row = rows.find((x) => x.round === r);
      if (!row) return "N/A".padEnd(w);
      const mark = row.event === "KILL" ? "*" : row.event === "DEAD" ? "~" : " ";
      const lat  = row.avgLatMs !== null ? `${row.avgLatMs.toFixed(1)}ms` : "—";
      return `${mark}${row.tps.toFixed(1).padStart(7)} TPS ${lat.padStart(8)}`.padEnd(w);
    });
    console.log(`  ${String(r).padStart(4)}  ${cols.join(" ")}`);
  }

  console.log(`\n${div}\n`);
}

// ── Main ───────────────────────────────────────────────────────────────────────
(async () => {
  console.log(`\n${"═".repeat(68)}`);
  console.log(`  Cabinet KV Store — §5.4 Failure Performance Test`);
  console.log(`  ${ts()}`);
  console.log(`${"═".repeat(68)}`);
  console.log(`  Gateway  : ${BASE}`);
  console.log(`  Rounds   : ${ROUNDS}/scenario | Ops/round: ${OPS_PER_ROUND} concurrent POSTs`);
  console.log(`  Metrics  : TPS (writes/sec) + Latency (avg ms per POST, client→consensus→DB→client)`);
  console.log(`\n  Scenarios (kill at round 5 | 4 rounds before / 7 rounds after):`);
  console.log(`    1. Strong Kill — Node 1 (port 9081, highest-weight follower)`);
  console.log(`    2. Weak Kill   — Node 2 (port 9082, lowest-weight follower)`);
  console.log(`    3. Random Kill — Node 1 or 2 (chosen randomly)`);
  console.log(`\n  Start cluster before each scenario:`);
  console.log(`    cd server && go run . -id 1      (Terminal 1)`);
  console.log(`    cd server && go run . -id 2      (Terminal 2)`);
  console.log(`    cd server && go run . -id 0 -gateway  (Terminal 3, last)`);
  console.log(`${"═".repeat(68)}\n`);

  // Connectivity check
  {
    let ready = false;
    for (let attempt = 1; attempt <= 15; attempt++) {
      try {
        const probe = await fetch(BASE + "/customers", { signal: AbortSignal.timeout(5000) });
        if (probe.ok) { ready = true; break; }
      } catch { /* not yet */ }
      console.log(`  Waiting for gateway... (${attempt}/15)`);
      await sleep(2000);
    }
    if (!ready) {
      console.error(`  ${R}Cannot reach ${BASE} after 30s. Is the cluster running?${X}`);
      process.exit(1);
    }
    console.log(`  ${G}Gateway reachable ✓${X}\n`);
  }

  const completed = [];
  const randomNode = Math.random() < 0.5 ? 1 : 2;
  const KILL_ROUNDS = [5];   // single kill at round 5; rounds after = recovery observation
  const plan = [
    { name: "Strong Kill",                      killRounds: KILL_ROUNDS, targetNode: 1 },
    { name: "Weak Kill",                        killRounds: KILL_ROUNDS, targetNode: 2 },
    { name: `Random Kill (Node ${randomNode})`, killRounds: KILL_ROUNDS, targetNode: randomNode },
  ];

  for (let i = 0; i < plan.length; i++) {
    const { name, killRounds, targetNode } = plan[i];

    if (i === 0) {
      await waitForEnter(`  Press Enter to start Scenario 1 (${name})...`);
    } else {
      console.log(`\n  ${Y}Scenario ${i} done. Restart ALL 3 nodes, then press Enter.${X}`);
      await waitForEnter(`  Enter to start Scenario ${i + 1} (${name})...`);

      // Wait for gateway to come back up
      for (let attempt = 1; attempt <= 15; attempt++) {
        try {
          const p = await fetch(BASE + "/customers", { signal: AbortSignal.timeout(2000) });
          if (p.ok) { console.log(`  ${G}Gateway reachable ✓${X}`); break; }
        } catch { /* retry */ }
        if (attempt === 15) { console.error(`  ${R}Gateway unreachable. Aborting.${X}`); process.exit(1); }
        console.log(`  Waiting for gateway... (${attempt}/15)`);
        await sleep(2000);
      }
    }

    const rows = await runScenario(name, killRounds, targetNode);
    completed.push({ name, killRounds, targetNode, rows });
  }

  printSummary(completed);
})();
