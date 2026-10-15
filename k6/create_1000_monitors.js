import http from 'k6/http';
import { check, sleep } from 'k6';

const baseURL = __ENV.BASE_URL || 'http://localhost:8080';
const token = __ENV.TOKEN;
const targetURL = __ENV.MONITOR_URL || 'https://api.open-meteo.com/v1/forecast?latitude=55.75&longitude=37.61&current=temperature_2m';
if (!token) throw new Error('TOKEN is required');

export const options = { vus: Number(__ENV.VUS || 50), iterations: Number(__ENV.MONITORS || 1000) };
export default function () {
  const response = http.post(`${baseURL}/me/monitors`, JSON.stringify({ url: targetURL, rules: [{ json_path: '$.current.temperature_2m', comparison_operator: '>', expected_value: -100 }] }), { headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', 'Idempotency-Key': `${__VU}-${__ITER}-${Date.now()}` } });
  check(response, { 'monitor created': (r) => r.status === 201 });
  sleep(0.01);
}
