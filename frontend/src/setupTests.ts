import "@testing-library/jest-dom";

// jsdom has neither a secure-context flag nor WebAuthn. Tests assume a
// capable page by default; the passkey availability tests override these.
Object.defineProperty(window, "isSecureContext", { value: true, configurable: true });
Object.defineProperty(window, "PublicKeyCredential", {
  value: function PublicKeyCredential() {},
  configurable: true,
  writable: true,
});
