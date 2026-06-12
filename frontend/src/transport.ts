// One gRPC-Web transport for the whole app, with a bearer interceptor.
//
// The access token lives in MEMORY only: localStorage would hand it to any
// XSS payload. A page refresh costs a re-login in this starter; stricter
// deployments move sessions into httpOnly cookies at the gateway.
import { createClient, type Interceptor } from "@connectrpc/connect";
import { createGrpcWebTransport } from "@connectrpc/connect-web";

import { ExampleService } from "./gen/example/v1/example_pb";
import { IdentityService } from "./gen/identity/v1/identity_pb";

let accessToken = "";
let refreshToken = "";

export function setTokens(access: string, refresh: string) {
  accessToken = access;
  refreshToken = refresh;
}

export function clearTokens() {
  accessToken = "";
  refreshToken = "";
}

export function currentRefreshToken(): string {
  return refreshToken;
}

// The jti claim identifies the access token for server-side revocation on
// logout. Decoding here is display-level only; the server never trusts it.
export function accessTokenID(): string {
  const parts = accessToken.split(".");
  if (parts.length !== 3) return "";
  try {
    const claims = JSON.parse(atob(parts[1].replace(/-/g, "+").replace(/_/g, "/")));
    return typeof claims.jti === "string" ? claims.jti : "";
  } catch {
    return "";
  }
}

const authInterceptor: Interceptor = (next) => (req) => {
  if (accessToken) {
    req.header.set("Authorization", `Bearer ${accessToken}`);
  }
  return next(req);
};

const transport = createGrpcWebTransport({
  baseUrl: import.meta.env.VITE_GATEWAY_URL ?? "http://localhost:8080",
  interceptors: [authInterceptor],
});

export const identity = createClient(IdentityService, transport);
export const notes = createClient(ExampleService, transport);
