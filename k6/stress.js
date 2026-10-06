// A ramp past the capacity of the stack, run on demand to find where it slows down. It only fails on errors
// and on a killed container, because latency is expected to climb once the stack saturates.
// Look at dropped_iterations and the latency per endpoint in the summary to see where the ceiling is.
export { setup, parent, teacher, login } from './lib.js';

export const options = {
  scenarios: {
    parents: {
      executor: 'ramping-arrival-rate',
      exec: 'parent',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 100,
      maxVUs: 400,
      stages: [
        { target: 50, duration: '1m' },
        { target: 120, duration: '1m' },
        { target: 200, duration: '1m' },
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
  },
};
