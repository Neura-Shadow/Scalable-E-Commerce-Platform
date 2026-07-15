import http from "k6/http";
import { check, fail, sleep } from "k6";
import { Counter, Rate } from "k6/metrics";

const BASE_URL = (__ENV.BASE_URL || "http://localhost:8888").replace(/\/$/, "");
const CUSTOMER_TOKEN = __ENV.CUSTOMER_TOKEN || "";
const ADMIN_TOKEN = __ENV.ADMIN_TOKEN || "";
const PRODUCT_ID = __ENV.PRODUCT_ID || "";
const NORMAL_ORDER_PRODUCT_ID = __ENV.NORMAL_ORDER_PRODUCT_ID || PRODUCT_ID;
const LIMITED_PRODUCT_ID = __ENV.LIMITED_PRODUCT_ID || PRODUCT_ID;
const IDEMPOTENCY_PRODUCT_ID = __ENV.IDEMPOTENCY_PRODUCT_ID || PRODUCT_ID;
const RATE_LIMIT_PRODUCT_ID = __ENV.RATE_LIMIT_PRODUCT_ID || PRODUCT_ID;
const EXPECTED_STOCK = numberEnv("EXPECTED_STOCK", 10);
const VUS = numberEnv("VUS", 1);
const DURATION = __ENV.DURATION || "10s";
const P95_MS = numberEnv("P95_MS", 750);
const P99_MS = numberEnv("P99_MS", 1500);
const enabledScenarios = scenarioSet(__ENV.SCENARIOS || "product-list,product-detail");

const unexpected5xx = new Counter("unexpected_5xx");
const transportFailures = new Counter("transport_failures");
const limitedOrderSuccesses = new Counter("limited_stock_order_successes");
const negativeStockObservations = new Counter("negative_stock_observations");
const expectedConflicts = new Counter("expected_conflicts");
const expectedRateLimits = new Counter("expected_rate_limits");
const idempotencyMismatches = new Counter("idempotency_mismatches");
const outboxErrorSamples = new Rate("outbox_error_samples");
const consumerErrorSamples = new Rate("consumer_error_samples");
const dlqGrowthSamples = new Rate("dlq_growth_samples");

const scenarios = {};
addScenario("product-list", "browseProductList", "0s");
addScenario("product-detail", "readProductDetail", "0s");
addScenario("normal-order", "placeNormalOrder", "2s");
addScenario("limited-stock", "placeLimitedStockOrder", "2s");
addScenario("idempotency", "retryIdempotentOrder", "4s");
addScenario("rate-limit", "exerciseRateLimit", "4s");
addScenario("outbox-publisher", "observeOutboxPublisher", "6s");
addScenario("redis-consumer", "observeRedisConsumer", "6s");

const thresholds = {
  checks: ["rate==1"],
  unexpected_5xx: ["count==0"],
  transport_failures: ["count==0"],
  negative_stock_observations: ["count==0"],
  idempotency_mismatches: ["count==0"],
  outbox_error_samples: ["rate==0"],
  consumer_error_samples: ["rate==0"],
  dlq_growth_samples: ["rate==0"],
  http_req_duration: [`p(95)<${P95_MS}`, `p(99)<${P99_MS}`],
};
if (isEnabled("limited-stock")) {
  thresholds.limited_stock_order_successes = [`count==${EXPECTED_STOCK}`];
}
if (booleanEnv("EXPECT_CONFLICTS", false)) {
  thresholds.expected_conflicts = ["count>0"];
}
if (isEnabled("rate-limit")) {
  thresholds.expected_rate_limits = ["count>0"];
}

export const options = {
  scenarios,
  thresholds,
  summaryTrendStats: ["avg", "med", "p(90)", "p(95)", "p(99)", "max"],
};

export function setup() {
  const livez = http.get(`${BASE_URL}/livez`, { tags: { endpoint: "livez" } });
  observeResponse(livez);
  if (!check(livez, { "livez is healthy": (response) => response.status === 200 })) {
    fail(`API liveness failed with status ${livez.status}`);
  }

  const readyz = http.get(`${BASE_URL}/readyz`, { tags: { endpoint: "readyz" } });
  observeResponse(readyz);
  if (!check(readyz, { "readyz is healthy": (response) => response.status === 200 })) {
    fail(`API readiness failed with status ${readyz.status}`);
  }

  if (requiresProduct() && !PRODUCT_ID) {
    fail("PRODUCT_ID is required for the selected scenarios");
  }
  if (requiresCustomerToken() && !CUSTOMER_TOKEN) {
    fail("CUSTOMER_TOKEN is required for order scenarios");
  }
  if (isEnabled("limited-stock") && !ADMIN_TOKEN) {
    fail("ADMIN_TOKEN is required for limited-stock inventory validation");
  }
  if (isEnabled("normal-order") && isEnabled("limited-stock") && NORMAL_ORDER_PRODUCT_ID === LIMITED_PRODUCT_ID) {
    fail("NORMAL_ORDER_PRODUCT_ID and LIMITED_PRODUCT_ID must differ when both scenarios run");
  }

  if (isEnabled("limited-stock")) {
    const inventory = limitedStockInventory("limited-stock-setup");
    const quantity = resultField(inventory, "quantity");
    if (!check(inventory, {
      "limited-stock fixture matches EXPECTED_STOCK": () => inventory.status === 200 && quantity === EXPECTED_STOCK,
    })) {
      fail(`limited-stock fixture quantity ${quantity} does not match EXPECTED_STOCK ${EXPECTED_STOCK}`);
    }
  }

  return { verifyLimitedStock: isEnabled("limited-stock") };
}

export function teardown(data) {
  if (!data || !data.verifyLimitedStock) {
    return;
  }

  const inventory = limitedStockInventory("limited-stock-teardown");
  const quantity = resultField(inventory, "quantity");
  check(inventory, {
    "limited-stock final quantity is zero": () => inventory.status === 200 && quantity === 0,
  });
}

export function browseProductList() {
  const response = http.get(`${BASE_URL}/api/v1/products?page=1&limit=20&order_by=name`, {
    tags: { endpoint: "product-list" },
  });
  observeResponse(response);
  check(response, { "product list returns 200": (res) => res.status === 200 });
  sleep(0.2);
}

export function readProductDetail() {
  const response = http.get(`${BASE_URL}/api/v1/products/${PRODUCT_ID}`, {
    tags: { endpoint: "product-detail" },
  });
  observeResponse(response);
  check(response, { "product detail returns 200": (res) => res.status === 200 });
  sleep(0.2);
}

export function placeNormalOrder() {
  const response = postOrder(NORMAL_ORDER_PRODUCT_ID, `normal-${__VU}-${__ITER}-${Date.now()}`);
  check(response, { "normal order succeeds": (res) => res.status === 200 });
  sleep(0.2);
}

export function placeLimitedStockOrder() {
  const response = postOrder(LIMITED_PRODUCT_ID, `limited-${__VU}-${__ITER}-${Date.now()}`);
  if (response.status === 200) {
    limitedOrderSuccesses.add(1);
  }
  if (response.status === 409) {
    expectedConflicts.add(1);
  }
  check(response, {
    "limited order has a safe status": (res) => [200, 409, 429].includes(res.status),
  });

  const inventory = limitedStockInventory("limited-stock-inventory");
  const quantity = resultField(inventory, "quantity");
  if (typeof quantity === "number" && quantity < 0) {
    negativeStockObservations.add(1);
  }
  check(inventory, {
    "inventory remains nonnegative": () => typeof quantity === "number" && quantity >= 0,
  });
  sleep(0.1);
}

function limitedStockInventory(endpoint) {
  const response = http.get(`${BASE_URL}/api/v1/inventory/${LIMITED_PRODUCT_ID}`, {
    headers: authorizationHeaders(ADMIN_TOKEN),
    tags: { endpoint },
  });
  observeResponse(response);
  return response;
}

export function retryIdempotentOrder() {
  const idempotencyKey = `retry-${__VU}-${__ITER}-${Date.now()}`;
  const first = postOrder(IDEMPOTENCY_PRODUCT_ID, idempotencyKey);
  const second = postOrder(IDEMPOTENCY_PRODUCT_ID, idempotencyKey);
  const firstOrderID = resultField(first, "id");
  const secondOrderID = resultField(second, "id");
  const matches = first.status === 200 && second.status === 200 && firstOrderID && firstOrderID === secondOrderID;
  if (!matches) {
    idempotencyMismatches.add(1);
  }
  check(second, { "idempotent retry returns the same order": () => Boolean(matches) });
  sleep(0.2);
}

export function exerciseRateLimit() {
  const response = postOrder(RATE_LIMIT_PRODUCT_ID, `rate-${__VU}-${__ITER}-${Date.now()}`);
  if (response.status === 429) {
    expectedRateLimits.add(1);
  }
  check(response, {
    "rate-limit scenario has a safe status": (res) => [200, 409, 429].includes(res.status),
  });
}

export function observeOutboxPublisher() {
  const response = metricsResponse("outbox-publisher");
  const body = response.body || "";
  const errors = sumMetric(body, "outbox_claim_failure_total")
    + sumMetric(body, "outbox_finalize_failure_total")
    + sumMetric(body, "outbox_publish_failure_total");
  outboxErrorSamples.add(errors > 0);
  check(response, {
    "publisher metrics are exposed": () => body.includes("outbox_publish_attempt_total"),
  });
  sleep(0.5);
}

export function observeRedisConsumer() {
  const response = metricsResponse("redis-consumer");
  const body = response.body || "";
  consumerErrorSamples.add(sumMetric(body, "outbox_consumer_failure_total") > 0);
  dlqGrowthSamples.add(sumMetric(body, "outbox_consumer_dead_letter_total") > 0);
  check(response, {
    "consumer metrics are exposed": () => body.includes("outbox_consumer_read_total"),
  });
  sleep(0.5);
}

function postOrder(productID, idempotencyKey) {
  const response = http.post(
    `${BASE_URL}/api/v1/orders`,
    JSON.stringify({ lines: [{ product_id: productID, quantity: 1 }] }),
    {
      headers: {
        ...authorizationHeaders(CUSTOMER_TOKEN),
        "Content-Type": "application/json",
        "Idempotency-Key": idempotencyKey,
      },
      tags: { endpoint: "order-placement" },
    },
  );
  observeResponse(response);
  return response;
}

function metricsResponse(endpoint) {
  const response = http.get(`${BASE_URL}/metrics`, { tags: { endpoint } });
  observeResponse(response);
  check(response, { "metrics endpoint returns 200": (res) => res.status === 200 });
  return response;
}

function observeResponse(response) {
  if (response.status === 0) {
    transportFailures.add(1);
  }
  if (response.status >= 500 && response.status <= 599) {
    unexpected5xx.add(1);
  }
}

function resultField(response, field) {
  try {
    const body = response.json();
    return body && body.result ? body.result[field] : undefined;
  } catch (_) {
    return undefined;
  }
}

function sumMetric(text, metricName) {
  return text
    .split("\n")
    .filter((line) => line.startsWith(metricName) && !line.startsWith("#"))
    .reduce((sum, line) => {
      const value = Number(line.trim().split(/\s+/).pop());
      return Number.isFinite(value) ? sum + value : sum;
    }, 0);
}

function authorizationHeaders(token) {
  if (!token) {
    return {};
  }
  return { Authorization: token.startsWith("Bearer ") ? token : `Bearer ${token}` };
}

function addScenario(name, exec, startTime) {
  if (!isEnabled(name)) {
    return;
  }
  scenarios[name] = {
    executor: "constant-vus",
    exec,
    vus: VUS,
    duration: DURATION,
    startTime,
    gracefulStop: "5s",
    tags: { scenario: name },
  };
}

function scenarioSet(value) {
  const selected = new Set(value.split(",").map((item) => item.trim()).filter(Boolean));
  if (selected.has("all")) {
    return new Set([
      "product-list",
      "product-detail",
      "normal-order",
      "limited-stock",
      "idempotency",
      "rate-limit",
      "outbox-publisher",
      "redis-consumer",
    ]);
  }
  return selected;
}

function isEnabled(name) {
  return enabledScenarios.has(name);
}

function requiresProduct() {
  return ["product-detail", "normal-order", "limited-stock", "idempotency", "rate-limit"].some(isEnabled);
}

function requiresCustomerToken() {
  return ["normal-order", "limited-stock", "idempotency", "rate-limit"].some(isEnabled);
}

function numberEnv(name, fallback) {
  const value = Number(__ENV[name] || fallback);
  if (!Number.isFinite(value) || value <= 0) {
    throw new Error(`${name} must be greater than zero`);
  }
  return value;
}

function booleanEnv(name, fallback) {
  const raw = __ENV[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  return raw.toLowerCase() === "true";
}
