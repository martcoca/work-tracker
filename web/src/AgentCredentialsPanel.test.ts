import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIError, type APIClient } from "./api";
import { fakeAuth, render, syntheticUser } from "./test-fixtures";
import type {
  AgentCredentialIssuedResponse,
  AgentCredentialListResponse,
  AgentCredentialMetadata,
  AgentCredentialMetadataResponse,
} from "./types";

const clock = new Date("2035-05-06T12:00:00Z");
const activeID = "a".repeat(32);
const alreadyRevokedID = "b".repeat(32);
const onceOnlyFixture = "synthetic-once-only-value";
let originalClipboard: Clipboard | undefined;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(clock);
  originalClipboard = navigator.clipboard;
});

afterEach(() => {
  vi.useRealTimers();
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: originalClipboard });
  vi.restoreAllMocks();
});

describe("agent credential surface", () => {
  it("creates the exact packet-bound workload and shows its value only for the first mounted view", async () => {
    const api = lifecycleAPI([]);
    const copy = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: copy } });
    const persistentWrite = vi.spyOn(Storage.prototype, "setItem");
    const cookieBefore = document.cookie;
    const { wrapper, router } = await render("/agent-credentials", fakeAuth(syntheticUser()).auth, api.client);

    await fillCredentialForm(wrapper);
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(api.createBody).toMatchObject({
      packet_id: "0004-E03-T01",
      attempt_id: "attempt-ui",
      workload: {
        tenant_id: "tenant-synthetic",
        issuer: "https://token.actions.example",
        subject: "repository:synthetic/work-tracker:ref:refs/heads/main",
      },
    });
    const requestedExpiry = new Date(String(api.createBody?.expires_at));
    expect(requestedExpiry.getTime() - clock.getTime()).toBe(45 * 60_000);
    expect(wrapper.find('[data-testid="one-time-credential"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("only time it will be shown");
    expect(wrapper.text()).toContain(activeID);

    await wrapper.get(".one-time-credential button").trigger("click");
    await flushPromises();
    expect(copy).toHaveBeenCalledOnce();
    expect(copy.mock.calls[0]?.[0] === onceOnlyFixture).toBe(true);
    expect(persistentWrite).not.toHaveBeenCalled();
    expect(document.cookie).toBe(cookieBefore);
    expect(router.currentRoute.value.fullPath).toBe("/agent-credentials");

    await router.push("/");
    await flushPromises();
    await router.push("/agent-credentials");
    await flushPromises();
    expect(wrapper.find('[data-testid="one-time-credential"]').exists()).toBe(false);
    expect(wrapper.text()).toContain(activeID);

    wrapper.unmount();
    const { wrapper: reloaded } = await render("/agent-credentials", fakeAuth(syntheticUser()).auth, api.client);
    expect(reloaded.find('[data-testid="one-time-credential"]').exists()).toBe(false);
    expect(reloaded.text()).toContain(activeID);
    reloaded.unmount();
  });

  it("lists every metadata field, keeps revoked rows, and updates a revocation without another read", async () => {
    const active = metadata(activeID);
    const alreadyRevoked = metadata(alreadyRevokedID, {
      revoked_at: "2035-05-06T12:20:00Z",
      revoked_by: "human-synthetic",
      last_used_at: "2035-05-06T12:10:00Z",
    });
    const api = lifecycleAPI([active, alreadyRevoked]);
    const { wrapper } = await render("/agent-credentials", fakeAuth(syntheticUser()).auth, api.client);

    for (const text of [
      activeID,
      "https://token.actions.example",
      "repository:synthetic/work-tracker:ref:refs/heads/main",
      "0004-E03-T01",
      "attempt-ui",
      "Never",
      "Revoked",
    ]) {
      expect(wrapper.text()).toContain(text);
    }
    expect(wrapper.findAll("tbody tr")).toHaveLength(2);

    await wrapper.get(`[data-revoke="${activeID}"]`).trigger("click");
    await flushPromises();

    expect(api.read).toHaveBeenCalledOnce();
    expect(wrapper.findAll("tbody tr")).toHaveLength(2);
    const updated = wrapper.get(`[data-credential-id="${activeID}"]`);
    expect(updated.text()).toContain("Revoked");
    expect(updated.text()).toContain("No action");
    expect(wrapper.get(`[data-credential-id="${alreadyRevokedID}"]`).text()).toContain("Revoked");
    wrapper.unmount();
  });

  it.each([
    [
      "workload_tenant_mismatch",
      403,
      "That workload belongs to another tenant. Use a workload for your signed-in tenant.",
    ],
    [
      "invalid_workload",
      422,
      "Enter an HTTPS issuer without a query or fragment, and a non-empty workload subject.",
    ],
    [
      "invalid_credential",
      422,
      "Choose an expiry from 1 through 60 minutes. Check that the packet ID and attempt are also valid.",
    ],
  ])("turns %s into distinct actionable guidance", async (code, status, message) => {
    const client: APIClient = {
      read: vi.fn().mockResolvedValue({ credentials: [] }),
      write: vi.fn().mockRejectedValue(new APIError(status, { code, message: "Server wording" })),
    };
    const { wrapper } = await render("/agent-credentials", fakeAuth(syntheticUser()).auth, client);
    await fillCredentialForm(wrapper);
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(wrapper.get("[role=alert]").text()).toContain(message);
    wrapper.unmount();
  });

  it("refuses an expiry beyond the maximum before making a request", async () => {
    const api = lifecycleAPI([]);
    const { wrapper } = await render("/agent-credentials", fakeAuth(syntheticUser()).auth, api.client);
    await fillCredentialForm(wrapper);
    await wrapper.get("#credential-lifetime").setValue("61");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(wrapper.get("[role=alert]").text()).toContain("1 through 60 minutes");
    expect(api.write).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});

async function fillCredentialForm(wrapper: Awaited<ReturnType<typeof render>>["wrapper"]): Promise<void> {
  await wrapper.get("#credential-packet").setValue("0004-E03-T01");
  await wrapper.get("#credential-attempt").setValue("attempt-ui");
  await wrapper.get("#credential-issuer").setValue("https://token.actions.example");
  await wrapper.get("#credential-subject").setValue("repository:synthetic/work-tracker:ref:refs/heads/main");
  await wrapper.get("#credential-lifetime").setValue("45");
}

function lifecycleAPI(initial: AgentCredentialMetadata[]) {
  let credentials = structuredClone(initial);
  let createBody: Record<string, unknown> | undefined;
  const read = vi.fn(async (path: string, _token: string) => {
    if (path !== "/api/agent-credentials") throw new Error(`unexpected read ${path}`);
    return { credentials: structuredClone(credentials) } satisfies AgentCredentialListResponse;
  });
  const write = vi.fn(async (method: "POST" | "PUT", path: string, _token: string, value: unknown) => {
    if (method !== "POST") throw new Error(`unexpected method ${method}`);
    if (path === "/api/agent-credentials") {
      createBody = structuredClone(value as Record<string, unknown>);
      const created = metadata(activeID);
      credentials = [created, ...credentials];
      return { credential: onceOnlyFixture, metadata: created } satisfies AgentCredentialIssuedResponse;
    }
    if (path === `/api/agent-credentials/${activeID}/revoke`) {
      const revoked = metadata(activeID, {
        revoked_at: "2035-05-06T12:30:00Z",
        revoked_by: "human-synthetic",
      });
      credentials = credentials.map((credential) => credential.id === activeID ? revoked : credential);
      return { metadata: revoked } satisfies AgentCredentialMetadataResponse;
    }
    throw new Error(`unexpected write ${path}`);
  });
  const client: APIClient = {
    async read<T>(path: string, token: string): Promise<T> {
      return (await read(path, token)) as T;
    },
    async write<T>(method: "POST" | "PUT", path: string, token: string, value: unknown): Promise<T> {
      return (await write(method, path, token, value)) as T;
    },
  };
  return { client, read, write, get createBody() { return createBody; } };
}

function metadata(id: string, overrides: Partial<AgentCredentialMetadata> = {}): AgentCredentialMetadata {
  return {
    id,
    tenant_id: "tenant-synthetic",
    workload: {
      kind: "workload",
      issuer: "https://token.actions.example",
      subject: "repository:synthetic/work-tracker:ref:refs/heads/main",
    },
    binding: {
      packet_id: "0004-E03-T01",
      attempt_id: "attempt-ui",
      issued_at: "2035-05-06T12:00:00Z",
      expires_at: "2035-05-06T12:45:00Z",
    },
    created_by: "human-synthetic",
    created_at: "2035-05-06T12:00:00Z",
    ...overrides,
  };
}
