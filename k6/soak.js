// A steady load held for a long time, run on demand, to find leaks and slow degradation. The latency limits are the
// same as in load.js, so a stack that gets slower over time fails them. The workflow also samples the memory of every
// container, and a memory use that keeps climbing shows up in the summary.
// Set DURATION to change the length, for example DURATION=5m for a quick check.
export { setup, parent, teacher, login } from './lib.js';

const duration = __ENV.DURATION || '30m';

export const options = {
  scenarios: {
    parents: {
      executor: 'constant-arrival-rate',
      exec: 'parent',
      rate: 30,
      timeUnit: '1s',
      duration,
      preAllocatedVUs: 60,
      maxVUs: 200,
    },
    teacher: {
      executor: 'constant-arrival-rate',
      exec: 'teacher',
      rate: 2,
      timeUnit: '1s',
      duration,
      preAllocatedVUs: 10,
      maxVUs: 50,
    },
    login: {
      executor: 'constant-arrival-rate',
      exec: 'login',
      rate: 1,
      timeUnit: '1s',
      duration,
      preAllocatedVUs: 5,
      maxVUs: 20,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    dropped_iterations: ['count<1'],
    'http_req_duration{endpoint:wali_dashboard}': ['p(95)<300'],
    'http_req_duration{endpoint:scan_claim}': ['p(95)<400'],
    'http_req_duration{endpoint:leaderboard}': ['p(95)<200'],
    'http_req_duration{endpoint:mood_log}': ['p(95)<300'],
    'http_req_duration{endpoint:guru_dashboard}': ['p(95)<300'],
    'http_req_duration{endpoint:mood_analytics}': ['p(95)<300'],
    'http_req_duration{endpoint:login}': ['p(95)<600'],
  },
};
