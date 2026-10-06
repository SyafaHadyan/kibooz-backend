// A ramping load that runs nightly and on demand. Every claim in one class takes the same ranking lock,
// so this also measures the worst case of many children claiming at once.
export { setup, parent, teacher, login } from './lib.js';

export const options = {
  scenarios: {
    parents: {
      executor: 'ramping-arrival-rate',
      exec: 'parent',
      startRate: 2,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 200,
      stages: [
        { target: 10, duration: '1m' },
        { target: 25, duration: '2m' },
        { target: 0, duration: '30s' },
      ],
    },
    teacher: {
      executor: 'constant-arrival-rate',
      exec: 'teacher',
      rate: 3,
      timeUnit: '1s',
      duration: '3m30s',
      preAllocatedVUs: 10,
      maxVUs: 50,
    },
    login: {
      executor: 'constant-arrival-rate',
      exec: 'login',
      rate: 1,
      timeUnit: '1s',
      duration: '3m30s',
      preAllocatedVUs: 5,
      maxVUs: 20,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    'http_req_duration{endpoint:wali_dashboard}': ['p(95)<800'],
    'http_req_duration{endpoint:scan_claim}': ['p(95)<1500'],
    'http_req_duration{endpoint:leaderboard}': ['p(95)<600'],
    'http_req_duration{endpoint:mood_log}': ['p(95)<800'],
    'http_req_duration{endpoint:guru_dashboard}': ['p(95)<800'],
    'http_req_duration{endpoint:mood_analytics}': ['p(95)<1000'],
    'http_req_duration{endpoint:login}': ['p(95)<1500'],
  },
};
