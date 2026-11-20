import { Code, ConnectError, createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { AdminService } from "./gen/proto/dmanager/v1/admin_pb";
import { AuthService } from "./gen/proto/dmanager/v1/auth_pb";
import { ContainerService } from "./gen/proto/dmanager/v1/container_pb";
import { LogService } from "./gen/proto/dmanager/v1/log_pb";
import { SettingsService } from "./gen/proto/dmanager/v1/settings_pb";

// The dev server does not proxy the API, so `pnpm dev` talks to the backend
// directly. Any production build is served by the backend and uses relative URLs.
export const getBaseUrl = () => (import.meta.env.DEV ? "http://localhost:9283" : "");

// AuthService procedures that legitimately return Unauthenticated without a
// session (wrong credentials, or simply not logged in). They must not be
// treated as an expired session.
const EXPECTED_UNAUTHENTICATED = new Set([
  `${AuthService.typeName}/GetServerStatus`,
  `${AuthService.typeName}/GetMe`,
  `${AuthService.typeName}/SetupAdmin`,
  `${AuthService.typeName}/Login`,
  `${AuthService.typeName}/Logout`,
  `${AuthService.typeName}/BeginPasskeyLogin`,
  `${AuthService.typeName}/FinishPasskeyLogin`,
]);

let unauthenticatedHandler: (() => void) | null = null;

/** Registers the callback run when an authenticated RPC returns Unauthenticated. */
export function setUnauthenticatedHandler(handler: (() => void) | null) {
  unauthenticatedHandler = handler;
}

const notifyIfUnauthenticated = (err: unknown, procedure: string) => {
  if (
    err instanceof ConnectError &&
    err.code === Code.Unauthenticated &&
    !EXPECTED_UNAUTHENTICATED.has(procedure)
  ) {
    unauthenticatedHandler?.();
  }
};

// Central place that notices an expired or revoked session mid-use, for unary
// calls and for errors raised while consuming a server stream.
export const sessionExpiryInterceptor: Interceptor = (next) => async (req) => {
  const procedure = `${req.service.typeName}/${req.method.name}`;
  let res;
  try {
    res = await next(req);
  } catch (err) {
    notifyIfUnauthenticated(err, procedure);
    throw err;
  }
  if (!res.stream) return res;
  const messages = res.message;
  return {
    ...res,
    message: (async function* () {
      try {
        yield* messages;
      } catch (err) {
        notifyIfUnauthenticated(err, procedure);
        throw err;
      }
    })(),
  };
};

// `vite --mode mock` (pnpm dev:mock) swaps the network transport for an
// in-browser one backed by fixtures. MODE is replaced at build time, so
// production builds drop the branch and never bundle src/mock.
export const transport =
  import.meta.env.MODE === "mock"
    ? (await import("./mock")).createBrowserMockTransport([sessionExpiryInterceptor])
    : createConnectTransport({
        baseUrl: getBaseUrl(),
        interceptors: [sessionExpiryInterceptor],
        useBinaryFormat: true,
        fetch: (url, options) => {
          return fetch(url, {
            ...options,
            credentials: "include",
          });
        },
      });

export const adminClient = createClient(AdminService, transport);
export const authClient = createClient(AuthService, transport);
export const containerClient = createClient(ContainerService, transport);
export const logClient = createClient(LogService, transport);
export const settingsClient = createClient(SettingsService, transport);
