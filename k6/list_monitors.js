import http from 'k6/http';
import { check, sleep } from 'k6';

const baseURL = __ENV.BASE_URL || 'http://localhost:8080';
const token = __ENV.TOKEN;
if (!token) throw new Error('TOKEN is required');

export const options = { vus: Number(__ENV.VUS || 50), duration: __ENV.DURATION || '1m' };
export default function () {
  const response = http.get(`${baseURL}/me/monitors?limit=100`, { headers: { Authorization: `Bearer ${token}` } });
  check(response, { 'monitor list returned': (r) => r.status === 200 });
  sleep(0.1);
}
