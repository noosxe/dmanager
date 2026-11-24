import { Code, ConnectError } from "@connectrpc/connect";

/**
 * Whether this page can use WebAuthn at all. Browsers expose
 * `PublicKeyCredential` only in secure contexts (HTTPS or localhost); on a
 * plain-HTTP LAN address it is missing and any attempt throws a TypeError.
 */
export function passkeysUnavailableReason(): string | null {
  if (typeof window === "undefined") {
    return null;
  }
  if (!window.isSecureContext) {
    return "Passkeys need a secure connection. Open dmanager over HTTPS (or on localhost) to use them.";
  }
  if (typeof window.PublicKeyCredential === "undefined") {
    return "This browser does not support passkeys.";
  }
  return null;
}

const errorName = (err: unknown): string | undefined => (err as { name?: string } | null)?.name;

/**
 * The user dismissed the browser prompt or it timed out (NotAllowedError), or
 * the ceremony was aborted (AbortError). Not a failure worth a red banner.
 */
export function isPasskeyCancel(err: unknown): boolean {
  if (err instanceof ConnectError) {
    return false;
  }
  const name = errorName(err);
  return name === "NotAllowedError" || name === "AbortError";
}

/**
 * Message for a failed passkey sign-in. Only the server's rate-limit message
 * (it carries the retry time) is shown verbatim; anything
 * else, including browser or library detail, becomes a generic message.
 */
export function passkeySignInErrorMessage(err: unknown): string {
  if (err instanceof ConnectError) {
    if (err.code === Code.ResourceExhausted) {
      return err.rawMessage;
    }
    return "Passkey sign-in failed. Try again or sign in with your password.";
  }
  if (errorName(err) === "InvalidStateError") {
    return "This authenticator can't be used right now. Try again.";
  }
  if (errorName(err) === "SecurityError") {
    return "Passkeys can't be used on this address. Open dmanager at the address it is configured for.";
  }
  return "Passkey sign-in failed. Try again or sign in with your password.";
}
