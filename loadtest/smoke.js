// k6 smoke test: quick connectivity + correctness check before a full load run.
//
//   k6 run loadtest/smoke.js
//
// Sends a few events, checks /health, then reads metrics back. Low volume so it
// won't trip the rate limiter on default settings.

import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  vus: 1,
  iterations: 1,
};

export default function () {
  const campaign = 'smoke_campaign';

  // 1. Ingest a known set of events.
  const events = [
    { type: 'click', campaign_id: campaign, user_id: 'u1' },
    { type: 'click', campaign_id: campaign, user_id: 'u2' },
    { type: 'impression', campaign_id: campaign, user_id: 'u1' },
    { type: 'conversion', campaign_id: campaign, user_id: 'u1' },
  ];
  const post = http.post(`${BASE}/events`, JSON.stringify(events), {
    headers: { 'Content-Type': 'application/json' },
  });
  check(post, { 'ingest -> 202': (r) => r.status === 202 });

  // 2. Health endpoint.
  const health = http.get(`${BASE}/health`);
  check(health, {
    'health -> 200': (r) => r.status === 200,
    'health reports ok': (r) => r.json('status') === 'ok',
  });

  // 3. Give the worker pool a moment to flush to the DB.
  sleep(2);

  // 4. Read metrics back.
  const metrics = http.get(`${BASE}/campaigns/${campaign}/metrics`);
  check(metrics, {
    'metrics -> 200': (r) => r.status === 200,
    'has clicks field': (r) => r.json('clicks') !== undefined,
  });
  console.log(`metrics response: ${metrics.body}`);
}
