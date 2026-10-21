// The chat socket under load. Parents hold a socket open and the teacher sends a message to the room of the class every second, so
// the run shows how long a message takes to reach a socket and whether the sockets stay open. It is started by hand through the
// Performance workflow, and it is not part of the checks of a pull request or of the nightly run. The limits are placeholders
// to calibrate on the first runs.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { WebSocket } from 'k6/websockets';
import { api, json, setup as baseSetup, tagged } from './lib.js';

const deliveryMs = new Trend('chat_delivery_ms', true);
const framesSeen = new Counter('chat_message_frames');
const socketsClosedEarly = new Counter('chat_sockets_closed_early');

export const options = {
  scenarios: {
    // each listener is one socket for the whole run, and the sender writes while every socket is up
    listeners: { executor: 'per-vu-iterations', exec: 'listener', vus: 15, iterations: 1, maxDuration: '90s' },
    sender: { executor: 'constant-vus', exec: 'sender', vus: 1, duration: '40s', startTime: '8s' },
  },
  thresholds: {
    checks: ['rate>0.99'],
    // 40 messages go to 15 sockets, so a run that delivers nothing or loses most frames must fail instead of passing on an empty trend
    chat_message_frames: ['count>=500'],
    chat_delivery_ms: ['p(95)<1000'],
    chat_sockets_closed_early: ['count==0'],
    'http_req_duration{endpoint:chat_send}': ['p(95)<400'],
  },
};

// setup makes the class and its parents like the other scripts do, and opens the room of the class as its teacher
export function setup() {
  const data = baseSetup();
  const dashboard = json(http.get(`${api}/guru/dashboard`, tagged(data.guru.token, 'guru_dashboard')));
  const classId = dashboard.data.classOverview.classId;

  const opened = http.post(`${api}/chat/conversations`, JSON.stringify({ classId }), tagged(data.guru.token, 'chat_open'));

  if (opened.status !== 200 && opened.status !== 201) {
    throw new Error(`opening the room failed with status ${opened.status} ${opened.body}`);
  }

  return { ...data, room: json(opened).data.id };
}

// listener is a parent whose app is open, so a socket stays up for 55 seconds and every message frame is timed
export function listener(data) {
  const me = data.parents[(__VU - 1) % data.parents.length];
  const url = `${api.replace(/^http/, 'ws')}/chat/ws`;
  const ws = new WebSocket(url, null, { headers: { Authorization: `Bearer ${me.token}` } });

  let ready = false;
  let closing = false;

  ws.addEventListener('message', (event) => {
    const frame = JSON.parse(event.data);

    if (frame.type === 'ready') {
      ready = true;

      return;
    }

    if (frame.type === 'message' && frame.message && String(frame.message.body).startsWith('k6:')) {
      framesSeen.add(1);
      const sentAt = Number(String(frame.message.body).slice(3));

      if (Number.isFinite(sentAt)) {
        deliveryMs.add(Date.now() - sentAt);
      }
    }
  });

  ws.addEventListener('close', () => {
    if (!closing) {
      socketsClosedEarly.add(1);
    }
  });

  setTimeout(() => {
    check(null, { 'the socket said ready': () => ready });
    closing = true;
    ws.close();
  }, 55000);
}

// sender is the teacher who writes to the class once a second
export function sender(data) {
  const res = http.post(
    `${api}/chat/conversations/${data.room}/messages`,
    JSON.stringify({ body: `k6:${Date.now()}` }),
    tagged(data.guru.token, 'chat_send'),
  );

  check(res, { 'message is 201': (r) => r.status === 201 });

  // the iteration lasts a second, so one message goes out each second
  sleep(Math.max(0, 1 - res.timings.duration / 1000));
}
