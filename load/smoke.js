import http from 'k6/http';
import { Counter, Trend } from 'k6/metrics';

const alg = __ENV.ALG;
const operation = __ENV.OPERATION;
const base = __ENV.BASE_URL;
const runId = __ENV.RUN_ID;
const metricsPath = __ENV.METRICS_PATH;
const token = __ENV.TOKEN;

if (
  !alg
  || !['issue', 'verify'].includes(operation)
  || !base
  || !runId
  || !metricsPath
  || (operation === 'verify' && !token)
) {
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

function responseIsSuccessful(response) {
  if (response.status !== 200) {
    return false;
  }

  try {
    const body = response.json();
    if (operation === 'issue') {
      const tokenParts = typeof body.token === 'string' ? body.token.split('.') : [];
      return tokenParts.length === 3 && tokenParts.every(Boolean);
    }
    return body.status === 'success';
  } catch (_) {
    return false;
  }
}

export default function () {
  const measureStart = Date.now() - 1;
  const measureEnd = measureStart + 60000;
  console.log(`PHASE_JSON:${JSON.stringify({
    scenario_start_ms: measureStart,
    measure_start_ms: measureStart,
    measure_end_ms: measureEnd,
    grace_end_ms: measureEnd + 30000,
  })}`);

  let start;
  let response;
  if (operation === 'issue') {
    const body = JSON.stringify({ sub: 'vu-0001' });
    start = Date.now();
    response = http.post(`${base}/token?alg=${alg}`, body, {
      headers: { 'Content-Type': 'application/json' },
      timeout: '30s',
    });
  } else {
    const authorization = `Bearer ${token}`;
    start = Date.now();
    response = http.get(`${base}/protected?alg=${alg}`, {
      headers: { Authorization: authorization },
      timeout: '30s',
    });
  }

  const end = Date.now();
  const success = responseIsSuccessful(response);

  if (!success) {
    throw new Error(`smoke request failed with HTTP ${response.status}`);
  }

  successfulStartedInWindow.add(1);
  successfulDuration.add(end - start);
  successfulInWindow.add(1);
}

export function handleSummary(data) {
  const completed = data.metrics.successful_in_window?.values.count || 0;
  const started = data.metrics.successful_started_in_window?.values.count || 0;
  const duration = data.metrics.successful_duration_ms?.values || {};
  const artifact = {
    schema_version: 1,
    run_id: runId,
    alg,
    operation,
    target_vu: 1,
    successful_in_window: completed,
    successful_started_in_window: started,
    mean_ms: duration.avg ?? null,
    p99_ms: duration['p(99)'] ?? null,
  };

  return { [metricsPath]: `${JSON.stringify(artifact, null, 2)}\n` };
}
