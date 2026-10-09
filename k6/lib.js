// Shared helpers and the scenarios used by smoke.js and load.js
import http from 'k6/http';
import { check } from 'k6';

/* global __ENV */

const base = __ENV.BASE_URL || 'http://127.0.0.1:8080';

export const api = `${base}/api/v1`;

const parentCount = 20;

// digits returns a string of random digits, so every run gets accounts with their own email
function digits(length) {
  let out = '';
  while (out.length < length) {
    out += Math.floor(Math.random() * 10);
  }

  return out;
}

// every run creates its own accounts with a throwaway password that only lives in memory
function newPassword() {
  return `k6${digits(24)}`;
}

// json reads the body of a response
export function json(res) {
  return res.json();
}

// tagged sends the bearer token and names the endpoint, so thresholds can target one endpoint at a time
export function tagged(token, endpoint) {
  const headers = { 'Content-Type': 'application/json' };

  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  return { headers, tags: { endpoint } };
}

// register creates an account with a random email and returns its data, and it throws when the registration fails
export function register(body, pass) {
  const res = http.post(
    `${api}/auth/register`,
    JSON.stringify({ email: `k6.${digits(14)}@example.com`, password: pass, fullName: 'K6 Account', ...body }),
    tagged('', 'register'),
  );

  if (res.status !== 201) {
    throw new Error(`registration failed with status ${res.status} ${res.body}`);
  }

  return { ...json(res).data, email: json(res).data.user.email };
}

// setup creates one teacher with a class and many parents in it once, because bcrypt makes registering slow.
// Every virtual user then reuses these accounts.
export function setup() {
  const pass = newPassword();
  const guru = register({ role: 'GURU', class: { name: 'k6', gradeLevel: 'Class K' } }, pass);
  const dashboard = json(http.get(`${api}/guru/dashboard`, tagged(guru.token, 'guru_dashboard')));
  const code = dashboard.data.classOverview.joinCode;

  const parents = [];

  for (let i = 0; i < parentCount; i++) {
    const wali = register(
      {
        role: 'WALI',
        classCode: code,
        student: { nisn: digits(12), fullName: `Child ${i}` },
      },
      pass,
    );
    const me = json(http.get(`${api}/wali/dashboard`, tagged(wali.token, 'wali_dashboard')));

    parents.push({ token: wali.token, email: wali.email, studentId: me.data.student.id });
  }

  // open the database connections before the measured part starts, otherwise the first burst pays for them
  http.batch(parents.map((item) => ['GET', `${api}/wali/dashboard`, null, tagged(item.token, 'warmup')]));

  return { guru, parents, password: pass };
}

// pick returns a random item of the list
function pick(list) {
  return list[Math.floor(Math.random() * list.length)];
}

// parent is the scenario of a parent who opens the dashboard, claims a scan and looks at the leaderboard
export function parent(data) {
  const me = pick(data.parents);

  check(http.get(`${api}/wali/dashboard`, tagged(me.token, 'wali_dashboard')), {
    'parent dashboard is 200': (r) => r.status === 200,
  });

  check(
    http.post(
      `${api}/trash/scan-claim`,
      JSON.stringify({ studentId: me.studentId, trashType: 'ORGANIK', confidenceScore: 0.9 }),
      tagged(me.token, 'scan_claim'),
    ),
    { 'claim is 200': (r) => r.status === 200 },
  );

  check(http.get(`${api}/leaderboard`, tagged(me.token, 'leaderboard')), {
    'leaderboard is 200': (r) => r.status === 200,
  });
}

// teacher is the scenario of a teacher who logs a mood and opens the dashboard and the mood analytics
export function teacher(data) {
  const kid = pick(data.parents);

  check(
    http.post(
      `${api}/guru/mood/log`,
      JSON.stringify({ studentId: kid.studentId, moodType: 'SENANG' }),
      tagged(data.guru.token, 'mood_log'),
    ),
    { 'mood log is 201': (r) => r.status === 201 },
  );

  check(http.get(`${api}/guru/dashboard`, tagged(data.guru.token, 'guru_dashboard')), {
    'teacher dashboard is 200': (r) => r.status === 200,
  });

  check(http.get(`${api}/guru/mood/analytics?range=weekly`, tagged(data.guru.token, 'mood_analytics')), {
    'analytics is 200': (r) => r.status === 200,
  });
}

// login is the expensive path because of bcrypt, so it runs at a low rate to track its cost
export function login(data) {
  const me = pick(data.parents);

  check(
    http.post(
      `${api}/auth/login`,
      JSON.stringify({ email: me.email, password: data.password, role: 'WALI' }),
      tagged('', 'login'),
    ),
    { 'login is 200': (r) => r.status === 200 },
  );
}
