/**
 * Functional Correctness Test Suite — Cabinet KV Store
 * Tests the 5 core API routes in sequence.
 * Run with: node test_functional.js
 */

const BASE = "http://localhost:8080";
const GREEN = "\x1b[92m", RED = "\x1b[91m", RESET = "\x1b[0m";
const PASS = `${GREEN}PASS${RESET}`, FAIL = `${RED}FAIL${RESET}`;

const results = [];

async function check(name, method, path, expectedStatus, body = null) {
  const url = BASE + path;
  const opts = { method, headers: { "Content-Type": "application/json" } };
  if (body) opts.body = JSON.stringify(body);

  try {
    const res = await fetch(url, opts);
    const ok = res.status === expectedStatus;
    let data = null;
    let detail = "";
    try {
      data = await res.json();
      if (Array.isArray(data)) {
        detail = `${data.length} records returned`;
      } else {
        detail = Object.keys(data).slice(0, 3).map(k => `${k}: ${JSON.stringify(data[k])}`).join(", ");
      }
    } catch { /* non-JSON body */ }

    results.push({ name, ok, expected: expectedStatus, actual: res.status });
    console.log(`  [${ok ? PASS : FAIL}] ${name.padEnd(35)} expected=${expectedStatus}  actual=${res.status}  ${detail}`);
    return data;
  } catch (err) {
    results.push({ name, ok: false, expected: expectedStatus, actual: "ERR" });
    console.log(`  [${FAIL}] ${name.padEnd(35)} ERROR: ${err.message}`);
    return null;
  }
}

const customer = {
  name: "Test User", age: 30, gender: "Male",
  address: "123 Test St", email: "test@example.com",
  phone_number: "555-0000", account_type: "Checking"
};
const updated = {
  name: "Updated User", age: 35, gender: "Male",
  address: "456 New Ave", email: "updated@example.com",
  phone_number: "999-9999", account_type: "Savings", account_balance: 500.0
};

(async () => {
  console.log(`\n${"=".repeat(65)}`);
  console.log(`  Cabinet KV Store — Functional Correctness Tests`);
  console.log(`  Target: ${BASE}`);
  console.log(`${"=".repeat(65)}\n`);

  // 1. GET all customers
  await check("GET /customers",           "GET",    "/customers",              200);

  // 2. POST new customer — save the returned ID for later tests
  const created = await check("POST /customers",          "POST",   "/customers",              201, customer);
  const id = created?.id ?? null;

  if (!id) {
    console.log(`\n  Cannot continue: POST did not return an ID.\n`);
    process.exit(1);
  }

  // 3. GET customer by ID
  await check("GET /customers/:id",       "GET",    `/customers/${id}`,        200);

  // 4. PUT (update) customer
  await check("PUT /customers/:id",       "PUT",    `/customers/${id}`,        200, updated);

  // 5. DELETE customer
  await check("DELETE /customers/:id",    "DELETE", `/customers/${id}`,        200);

  // ── summary ───────────────────────────────────────────────────────────────
  const total  = results.length;
  const passed = results.filter(r => r.ok).length;
  const failed = total - passed;

  console.log(`\n${"=".repeat(65)}`);
  console.log(`  Results: ${passed}/${total} passed  (${Math.round(passed / total * 100)}% pass rate)`);
  console.log(`${"=".repeat(65)}\n`);

  process.exit(failed === 0 ? 0 : 1);
})();
