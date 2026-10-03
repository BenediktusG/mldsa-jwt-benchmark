import http from 'k6/http';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';

const alg = __ENV.ALG;
const operation = __ENV.OPERATION;
const target = Number(__ENV.TARGET_VU);
const runId = __ENV.RUN_ID;
const base = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const subjectPrefix = __ENV.SUBJECT_PREFIX || 'vu-';
const subjectWidth = Number(__ENV.SUBJECT_WIDTH || '4');
const tokens = operation === 'verify' ? JSON.parse(open(__ENV.TOKENS_FILE)) : null;

if (!['issue', 'verify'].includes(operation) || ![1, 10, 100, 1000].includes(target) || !alg || !runId || (tokens && tokens.length !== target)) {
  throw new Error('invalid scenario configuration');
}

export const successfulInWindow = new Counter('successful_in_window');
export const successfulDuration = new Trend('successful_duration_ms', true);

export const options = {
  scenarios: {
    experiment: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [{ duration: '15s', target }, { duration: '60s', target }],
      gracefulStop: '30s',
    },
  },
  noConnectionReuse: false,
  discardResponseBodies: false,
  thresholds: {},
  summaryTrendStats: ['avg', 'p(99)'],
};

function subject(id) {
  return subjectPrefix + String(id).padStart(subjectWidth, '0');
}

export default function () {
  const vu = exec.vu.idInTest;
  if (vu < 1 || vu > target) throw new Error('VU out of range');
  const measureStart = exec.scenario.startTime + 15000;
  const measureEnd = measureStart + 60000;
  if (vu === 1 && exec.vu.iterationInScenario === 0) {
    console.log('PHASE_JSON:' + JSON.stringify({ scenario_start_ms: exec.scenario.startTime, measure_start_ms: measureStart, measure_end_ms: measureEnd, grace_end_ms: measureEnd + 30000 }));
  }
  let start;
  let response;
  if (operation === 'issue') {
    const body = JSON.stringify({ sub: subject(vu) });
    start = Date.now();
    response = http.post(`${base}/token?alg=${alg}`, body, {
      headers: { 'Content-Type': 'application/json' }, timeout: '30s',
    });
  } else {
    const authorization = `Bearer ${tokens[vu - 1]}`;
    start = Date.now();
    response = http.get(`${base}/protected?alg=${alg}`, {
      headers: { Authorization: authorization }, timeout: '30s',
    });
  }
  const end = Date.now();
  let success = response.status === 200;
  if (success) {
    try {
      const body = response.json();
      success = operation === 'issue'
        ? typeof body.token === 'string' && body.token.split('.').length === 3 && body.token.split('.').every(Boolean)
        : body.status === 'success';
    } catch (_) { success = false; }
  }
  const tags = { run_id: runId, alg, operation, target_vu: String(target), start_ms: String(start), end_ms: String(end), measure_start_ms: String(measureStart), measure_end_ms: String(measureEnd) };
  if (success && start >= measureStart && start < measureEnd) {
    successfulDuration.add(end - start, tags);
    if (end < measureEnd) successfulInWindow.add(1, tags);
  }
}
