import http from 'k6/http';
import {check} from 'k6';
export const options={vus:1000,duration:'30s'};
export default function(){const r=http.get(`${__ENV.BASE_URL}/healthz`);check(r,{'health 200':x=>x.status===200});}
