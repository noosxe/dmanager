import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { AuthService } from "./gen/proto/dmanager/v1/auth_pb";
import { ContainerService } from "./gen/proto/dmanager/v1/container_pb";
import { LogService } from "./gen/proto/dmanager/v1/log_pb";
import { SettingsService } from "./gen/proto/dmanager/v1/settings_pb";

const getBaseUrl = () => {
  if (typeof window !== "undefined") {
    // If served by the Go backend directly, use relative URL
    if (window.location.port !== "5173" && window.location.port !== "3000") {
      return "";
    }
  }
  return "http://localhost:9283";
};

export const transport = createConnectTransport({
  baseUrl: getBaseUrl(),
  useBinaryFormat: true,
  fetch: (url, options) => {
    return fetch(url, {
      ...options,
      credentials: "include",
    });
  },
});

export const authClient = createClient(AuthService, transport);
export const containerClient = createClient(ContainerService, transport);
export const logClient = createClient(LogService, transport);
export const settingsClient = createClient(SettingsService, transport);
