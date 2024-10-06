import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
    vus: 10,
    duration: '60s',
}

export default function () {
    const res = http.get('http://114.51.4.2:8001');
    check(res, { 'status was 200': (r) => r.status == 200 });
}