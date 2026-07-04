import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AuthService } from "./gen/proto/dmanager/v1/auth_pb";

const getBaseUrl = () => {
  if (typeof window !== "undefined") {
    // If served by the Go backend directly, use relative URL
    if (window.location.port !== "5173" && window.location.port !== "3000") {
      return "";
    }
  }
  return "http://localhost:8080";
};

export const transport = createConnectTransport({
  baseUrl: getBaseUrl(),
  fetch: (url, options) => {
    return fetch(url, {
      ...options,
      credentials: "include",
    });
  },
});

export const authClient = createClient(AuthService, transport);

