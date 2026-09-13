import { expect, it } from "vitest";
import { AUTH_DOMAIN, CALLBACK_URL, LOGOUT_URL, tenantIDFromClaims } from "./auth";

it("uses the settled production host for Identity Platform returns", () => {
  expect(AUTH_DOMAIN).toBe("tracker.martcoca.com");
  expect(CALLBACK_URL).toBe("https://tracker.martcoca.com/__/auth/handler");
  expect(LOGOUT_URL).toBe("https://tracker.martcoca.com/signed-out");
});

it("derives the workload tenant from the signed custom claim", () => {
  expect(tenantIDFromClaims({ tenant_id: "tenant-synthetic" })).toBe("tenant-synthetic");
  expect(tenantIDFromClaims({ tenant_id: "" })).toBeNull();
  expect(tenantIDFromClaims({ tenant_id: " tenant-synthetic " })).toBeNull();
  expect(tenantIDFromClaims({ tenant_id: 42 })).toBeNull();
  expect(tenantIDFromClaims({})).toBeNull();
});
