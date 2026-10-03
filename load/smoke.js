import http from 'k6/http';
import { Counter, Trend } from 'k6/metrics';

const alg = __ENV.ALG;
const operation = __ENV.OPERATION;
const base = __ENV.BASE_URL;
const runId = __ENV.RUN_ID;
const token = __ENV.TOKEN;
const metricsPath = __ENV.METRICS_PATH;

if (!alg || !base || !runId || !metricsPath || !['issue', 'verify'].includes(operation) || (operation === 'verify' && !token)) {
  throw new Error('invalid smoke-test configuration');
}

export const successfulInWindow = new Counter('successful_in_window');
export const successfulStartedInWindow = new Counter('successful_started_in_window');
export const successfulDuration = new Trend('successful_duration_ms', true);

export const options = {
  vus: 1,
  iterations: 1,
  discardResponseBodies: false,
  thresholds: {},
  summaryTrendStats: ['avg', 'p(99)'],
};

export default function () {
  const measureStart = Date.now() - 1;
  const measureEnd = measureStart + 60000;
  console.log(`PHASE_JSON:${JSON.stringify({
    scenario_start_ms: measureStart,
    measure_start_ms: measureStart,
    measure_end_ms: measureEnd,
    grace_end_ms: measureEnd + 30000,
  })}`);

  const start = Date.now();
  const response = operation === 'issue'
    ? http.post(`${base}/token?alg=${alg}`, JSON.stringify({ sub: 'vu-0001' }), {
      headers: { 'Content-Type': 'application/json' },
    })
    : http.get(`${base}/protected?alg=${alg}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
  const end = Date.now();

  let success = response.status === 200;
  if (success) {
    try {
      const body = response.json();
      success = operation === 'issue'
        ? typeof body.token === 'string' && body.token.split('.').length === 3
        : body.status === 'success';
    } catch (_) {
      success = false;
    }
  }
  if (!success) throw new Error(`smoke request failed with HTTP ${response.status}`);

  successfulStartedInWindow.add(1);
  successfulDuration.add(end - start);
  successfulInWindow.add(1);
}

export function handleSummary(data) {
  const duration = data.metrics.successful_duration_ms?.values || {};
  return {
    [metricsPath]: `${JSON.stringify({
      schema_version: 1,
      run_id: runId,
      alg,
      operation,
      target_vu: 1,
      successful_in_window: data.metrics.successful_in_window?.values.count || 0,
      successful_started_in_window: data.metrics.successful_started_in_window?.values.count || 0,
      mean_ms: duration.avg ?? null,
      p99_ms: duration['p(99)'] ?? null,
    }, null, 2)}\n`,
  };
}
