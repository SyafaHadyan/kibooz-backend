// A sudden burst well above the ceiling that stress.js finds, run on demand. A whole class opening the app at the same
// moment looks like this. The burst must not cause errors, and the steady traffic before and after it shows that the
// stack slows down under the burst and then returns to normal.
export { setup, parent, teacher } from './lib.js';

export const options = {
  scenarios: {
    before: {
      executor: 'constant-arrival-rate',
      exec: 'parent',
      rate: 10,
      timeUnit: '1s',
      duration: '20s',
      preAllocatedVUs: 30,
      maxVUs: 100,
    },
    burst: {
      executor: 'ramping-arrival-rate',
      exec: 'parent',
      startTime: '20s',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 200,
      maxVUs: 600,
      stages: [
        { target: 150, duration: '5s' },
        { target: 150, duration: '20s' },
        { target: 10, duration: '5s' },
      ],
    },
    recovery: {
      executor: 'constant-arrival-rate',
      exec: 'parent',
      // a short gap lets the queue of the burst drain, so this measures a stack that recovered
      startTime: '55s',
      rate: 10,
      timeUnit: '1s',
      duration: '40s',
      preAllocatedVUs: 30,
      maxVUs: 100,
    },
    teacher: {
      executor: 'constant-arrival-rate',
      exec: 'teacher',
      rate: 3,
      timeUnit: '1s',
      duration: '95s',
      preAllocatedVUs: 10,
      maxVUs: 50,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    'http_req_duration{scenario:before}': ['p(95)<1000'],
    'http_req_duration{scenario:recovery}': ['p(95)<1000'],
  },
};
