// A steady load that runs nightly and on demand, sized below the ceiling that stress.js finds so it should pass.
// Every claim in one class takes the same ranking lock, so this also covers many children claiming at once.
export { setup, parent, teacher, login } from './lib.js';

export const options = {
  scenarios: {
    parents: {
      executor: 'ramping-arrival-rate',
      exec: 'parent',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 60,
      maxVUs: 200,
      stages: [
        { target: 50, duration: '30s' },
        { target: 50, duration: '2m30s' },
        { target: 0, duration: '15s' },
      ],
    },
    teacher: {
      executor: 'constant-arrival-rate',
      exec: 'teacher',
      rate: 3,
      timeUnit: '1s',
      duration: '3m15s',
      preAllocatedVUs: 10,
      maxVUs: 50,
    },
    login: {
      executor: 'constant-arrival-rate',
      exec: 'login',
      rate: 1,
      timeUnit: '1s',
      duration: '3m15s',
      preAllocatedVUs: 5,
      maxVUs: 20,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    // dropped iterations mean the stack could not keep up with the arrival rate
    dropped_iterations: ['count<1'],
    'http_req_duration{endpoint:wali_dashboard}': ['p(95)<800'],
    'http_req_duration{endpoint:scan_claim}': ['p(95)<1500'],
    'http_req_duration{endpoint:leaderboard}': ['p(95)<600'],
    'http_req_duration{endpoint:mood_log}': ['p(95)<800'],
    'http_req_duration{endpoint:guru_dashboard}': ['p(95)<800'],
    'http_req_duration{endpoint:mood_analytics}': ['p(95)<1000'],
    'http_req_duration{endpoint:login}': ['p(95)<1500'],
  },
};
