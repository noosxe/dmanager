import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, describe, expect, it } from "vitest";

import { isPasskeyCancel, passkeySignInErrorMessage, passkeysUnavailableReason } from "./passkeys";

const GENERIC = "Passkey sign-in failed. Try again or sign in with your password.";

describe("passkeysUnavailableReason", () => {
  const original = Object.getOwnPropertyDescriptor(window, "PublicKeyCredential");
  afterEach(() => {
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true });
    if (original) Object.defineProperty(window, "PublicKeyCredential", original);
  });

  it("is null on a capable page", () => {
    expect(passkeysUnavailableReason()).toBeNull();
  });

  it("explains an insecure context", () => {
    Object.defineProperty(window, "isSecureContext", { value: false, configurable: true });
    expect(passkeysUnavailableReason()).toMatch(/secure connection/);
  });

  it("explains a missing PublicKeyCredential", () => {
    Object.defineProperty(window, "PublicKeyCredential", { value: undefined, configurable: true });
    expect(passkeysUnavailableReason()).toMatch(/does not support passkeys/);
  });
});

describe("isPasskeyCancel", () => {
  it("treats NotAllowedError and AbortError as a cancel", () => {
    expect(isPasskeyCancel(new DOMException("denied", "NotAllowedError"))).toBe(true);
    expect(isPasskeyCancel(new DOMException("aborted", "AbortError"))).toBe(true);
  });

  it("does not treat other errors, or server errors, as a cancel", () => {
    expect(isPasskeyCancel(new DOMException("x", "InvalidStateError"))).toBe(false);
    expect(isPasskeyCancel(new ConnectError("nope", Code.Unauthenticated))).toBe(false);
    expect(isPasskeyCancel(new Error("boom"))).toBe(false);
  });
});

describe("passkeySignInErrorMessage", () => {
  it("shows the server's rate-limit message and a friendly sentence for a failed sign-in", () => {
    expect(
      passkeySignInErrorMessage(
        new ConnectError(
          "too many failed login attempts, please try again in 30 seconds",
          Code.ResourceExhausted,
        ),
      ),
    ).toMatch(/try again in 30 seconds/);
    expect(
      passkeySignInErrorMessage(new ConnectError("passkey sign-in failed", Code.Unauthenticated)),
    ).toBe(GENERIC);
  });

  it("never shows detail from other server or browser errors", () => {
    expect(
      passkeySignInErrorMessage(
        new ConnectError("Backup Eligible flag inconsistency detected", Code.Internal),
      ),
    ).toBe(GENERIC);
    expect(passkeySignInErrorMessage(new TypeError("navigator.credentials is undefined"))).toBe(
      GENERIC,
    );
  });

  it("maps InvalidStateError to a short message", () => {
    expect(passkeySignInErrorMessage(new DOMException("x", "InvalidStateError"))).toMatch(
      /can't be used right now/,
    );
  });
});
