import http from 'k6/http';
import { check, sleep } from 'k6';

const target = 'http://114.51.4.2:8001'

// コマンドライン上でvusを指定
export const options = {
    vus: 50,
    duration: '60s',
}

export default function () {
    const res = http.get(target);
    check(res, { 'status was 200': (r) => r.status == 200 });
}