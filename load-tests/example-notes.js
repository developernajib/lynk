// k6 gRPC baseline for the core service's example module. This measures a
// SINGLE instance's baseline (run against one pod/process); cluster
// throughput is a deployment property, not something one script proves.
//
// Run (k6 with gRPC support):
//   k6 run -e CORE_ADDR=localhost:50051 load-tests/example-notes.js
//
// The test calls core directly and injects the trusted identity header the
// gateway would normally set; core sits behind the gateway and mTLS in real
// deployments, so this is the documented test-only shortcut.
import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';

const client = new grpc.Client();
client.load(['../services/core/proto'], 'example/v1/example.proto');

export const options = {
  scenarios: {
    write_burst: {
      executor: 'constant-vus',
      exec: 'createNote',
      vus: 10,
      duration: '30s',
    },
    read_flood: {
      executor: 'constant-vus',
      exec: 'listNotes',
      vus: 50,
      duration: '30s',
    },
  },
  thresholds: {
    // Single-pod baseline targets; tune to the deployment hardware.
    'grpc_req_duration{scenario:read_flood}': ['p(99)<50'],
    'grpc_req_duration{scenario:write_burst}': ['p(99)<200'],
  },
};

const ADDR = __ENV.CORE_ADDR || 'localhost:50051';
const IDENTITY = { 'x-user-id': '01970000-0000-7000-8000-000000000001' };

export function createNote() {
  client.connect(ADDR, { plaintext: true, reflect: false });
  const res = client.invoke(
    'example.v1.ExampleService/CreateNote',
    { title: `load test ${__VU}-${__ITER}`, body: 'k6 baseline' },
    { metadata: IDENTITY },
  );
  check(res, { 'create ok': (r) => r && r.status === grpc.StatusOK });
  client.close();
  sleep(0.1);
}

export function listNotes() {
  client.connect(ADDR, { plaintext: true, reflect: false });
  const res = client.invoke(
    'example.v1.ExampleService/ListNotes',
    { limit: 20, offset: 0 },
    { metadata: IDENTITY },
  );
  check(res, { 'list ok': (r) => r && r.status === grpc.StatusOK });
  client.close();
  sleep(0.05);
}
