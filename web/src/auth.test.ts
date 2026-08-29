import { expect, it } from "vitest";
import { AUTH_DOMAIN, CALLBACK_URL, LOGOUT_URL } from "./auth";

it("uses the settled production host for Identity Platform returns", () => {
  expect(AUTH_DOMAIN).toBe("tracker.martcoca.com");
  expect(CALLBACK_URL).toBe("https://tracker.martcoca.com/__/auth/handler");
  expect(LOGOUT_URL).toBe("https://tracker.martcoca.com/signed-out");
});
