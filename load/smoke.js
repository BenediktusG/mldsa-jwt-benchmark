import http from 'k6/http';
import { Counter, Trend } from 'k6/metrics';

const alg = __ENV.ALG;
const operation = __ENV.OPERATION;
const base = __ENV.BASE_URL;
const runId = __ENV.RUN_ID;
const token = __ENV.TOKEN;

if (!alg || !base || !runId || !['issue', 'verify'].includes(operation) || (operation === 'verify' && !token)) {
  throw new Error('invalid smoke-test configuration');
}

export const successfulInWindow = new Counter('successful_in_window');
export const successfulDuration = new Trend('successful_duration_ms', true);

export const options = {
  vus: 1,
  iterations: 1,
  discardResponseBodies: false,
  thresholds: {},
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

  const tags = {
    run_id: runId,
    alg,
    operation,
    target_vu: '1',
    start_ms: String(start),
    end_ms: String(end),
    measure_start_ms: String(measureStart),
    measure_end_ms: String(measureEnd),
  };
  successfulDuration.add(end - start, tags);
  successfulInWindow.add(1, tags);
}
