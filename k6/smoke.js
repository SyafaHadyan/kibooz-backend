// A 30 second check that runs on every pull request. The limits are placeholders to calibrate on the first runs.
export { setup, parent, teacher } from './lib.js';

export const options = {
  scenarios: {
    parents: { executor: 'constant-vus', exec: 'parent', vus: 5, duration: '30s' },
    teacher: { executor: 'constant-vus', exec: 'teacher', vus: 2, duration: '30s' },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    'http_req_duration{endpoint:wali_dashboard}': ['p(95)<500'],
    'http_req_duration{endpoint:scan_claim}': ['p(95)<800'],
    'http_req_duration{endpoint:leaderboard}': ['p(95)<400'],
    'http_req_duration{endpoint:mood_log}': ['p(95)<500'],
    'http_req_duration{endpoint:guru_dashboard}': ['p(95)<500'],
    'http_req_duration{endpoint:mood_analytics}': ['p(95)<600'],
  },
};
