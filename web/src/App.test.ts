import { flushPromises } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { APIError, type APIClient } from "./api";
import { directory, fakeAPI, fakeAuth, render, syntheticUser } from "./test-fixtures";

describe("read-only human surface", () => {
  it("signs a synthetic human in and navigates initiative to epic to packet", async () => {
    const signedOut = fakeAuth(null);
    const { wrapper: signInWrapper } = await render("/", signedOut.auth, fakeAPI());
    expect(signInWrapper.get("h1").text()).toContain("See the work");
    await signInWrapper.get("button").trigger("click");
    expect(signedOut.signIn).toHaveBeenCalledOnce();
    signInWrapper.unmount();

    const { wrapper, router } = await render("/", fakeAuth(syntheticUser()).auth, fakeAPI());
    expect(wrapper.text()).toContain("Initiative 0004");
    expect(wrapper.text()).toContain("1 unclaimed");

    await router.push("/initiatives/0004");
    await flushPromises();
    expect(wrapper.text()).toContain("Epic E02");

    await router.push("/initiatives/0004/epics/E02");
    await flushPromises();
    expect(wrapper.text()).toContain("Nobody has taken this packet");

    await router.push("/initiatives/0004/epics/E02/packets/0004-E02-T01");
    await flushPromises();
    for (const text of ["Frozen body", "Synthetic readable goal", "not started", "Full history", "Synthetic comment", "Comments"]) {
      expect(wrapper.text()).toContain(text);
    }
    wrapper.unmount();
  });

  it("renders cross-tenant refusal without the requested packet", async () => {
    const api: APIClient = {
      read: vi.fn().mockRejectedValue(new APIError(404, { code: "not_found", message: "That item is not available to this tenant." })),
    };
    const { wrapper } = await render("/initiatives/0005/epics/E01/packets/0005-E01-T01", fakeAuth(syntheticUser()).auth, api);
    expect(wrapper.get("[role=alert]").text()).toContain("not available to this tenant");
    expect(wrapper.text()).not.toContain("Tenant B private goal");
    wrapper.unmount();
  });

  it("states the age of an expired held directory and exposes no work", async () => {
    const stale = { ...directory, age_seconds: 7200, stale: true, expired_by_seconds: 3600 };
    const api: APIClient = {
      read: vi.fn().mockRejectedValue(
        new APIError(503, {
          code: "directory_stale",
          message: "The tenant directory is stale; no tenant data is being shown.",
          directory: stale,
        }),
      ),
    };
    const { wrapper } = await render("/", fakeAuth(syntheticUser()).auth, api);
    expect(wrapper.text()).toContain("Tenant directory stale");
    expect(wrapper.text()).toContain("2 hours old");
    expect(wrapper.text()).toContain("expired 1 hours ago");
    expect(wrapper.text()).not.toContain("Synthetic readable goal");
    wrapper.unmount();
  });
});
