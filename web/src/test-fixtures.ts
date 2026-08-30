import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { vi } from "vitest";
import App from "./App.vue";
import type { APIClient } from "./api";
import type { AuthPort, AuthUser } from "./auth";
import { routes } from "./router";
import type { DirectoryStatus, EpicView, InitiativeView, InitiativesView, PacketView } from "./types";

export const directory: DirectoryStatus = {
  published_at: "2035-05-06T12:00:00Z",
  expires_at: "2035-05-06T13:00:00Z",
  age_seconds: 1800,
  stale: false,
};

const views: Record<string, InitiativesView | InitiativeView | EpicView | PacketView> = {
  "/api/initiatives": {
    directory,
    initiatives: [{ id: "0004", epic_count: 1, packet_count: 1, blocked_count: 0, unclaimed_count: 1 }],
  },
  "/api/initiatives/0004": {
    directory,
    id: "0004",
    epics: [{ id: "E02", packet_count: 1, blocked_count: 0, unclaimed_count: 1 }],
  },
  "/api/initiatives/0004/epics/E02": {
    directory,
    initiative_id: "0004",
    id: "E02",
    packets: [{ id: "0004-E02-T01", status: "not started", taken_by: null, blocked: false, unclaimed: true }],
  },
  "/api/initiatives/0004/epics/E02/packets/0004-E02-T01": {
    directory,
    packet: {
      id: "0004-E02-T01",
      tenant_id: "tenant-synthetic",
      goal: "Synthetic readable goal",
      boundary: "Read only",
      done_when: "Synthetic checks pass",
      check: "npm test",
      context: "Synthetic context",
      status: "not started",
      version: 2,
      taken_by: null,
      comments: [{ event_id: "comment-1", timestamp: "2035-05-06T12:10:00Z", actor: "human-synthetic", text: "Synthetic comment" }],
      evidence: [],
      parent_id: null,
      superseded_by: null,
      history: [
        {
          kind: "packet issued", event_id: "event-1", timestamp: "2035-05-06T12:00:00Z", actor: "human-synthetic",
          tenant_id: "tenant-synthetic",
          body: { goal: "Synthetic readable goal", boundary: "Read only", done_when: "Synthetic checks pass", check: "npm test", context: "Synthetic context" },
        },
        { kind: "packet commented", event_id: "comment-1", timestamp: "2035-05-06T12:10:00Z", actor: "human-synthetic", text: "Synthetic comment" },
      ],
    },
  },
};

export async function render(path: string, auth: AuthPort, api: APIClient): Promise<{ wrapper: VueWrapper; router: ReturnType<typeof createRouter> }> {
  const router = createRouter({ history: createMemoryHistory(), routes });
  await router.push(path);
  await router.isReady();
  const wrapper = mount(App, { props: { auth, api }, attachTo: document.body, global: { plugins: [router] } });
  await flushPromises();
  return { wrapper, router };
}

export function fakeAPI(): APIClient {
  const read = vi.fn(async (path: string) => {
    const view = views[path];
    if (view === undefined) throw new Error(`unexpected path ${path}`);
    return structuredClone(view);
  });
  return {
    async read<T>(path: string): Promise<T> {
      return (await read(path)) as T;
    },
    async write<T>(_method: "POST" | "PUT", path: string): Promise<T> {
      throw new Error(`unexpected write ${path}`);
    },
  };
}

export function syntheticUser(): AuthUser {
  return { getToken: vi.fn().mockResolvedValue("synthetic-token") };
}

export function fakeAuth(user: AuthUser | null): { auth: AuthPort; signIn: ReturnType<typeof vi.fn>; signOut: ReturnType<typeof vi.fn> } {
  const signIn = vi.fn().mockResolvedValue(undefined);
  const signOut = vi.fn().mockResolvedValue(undefined);
  return {
    signIn,
    signOut,
    auth: {
      observe(callback) {
        callback(user);
        return () => undefined;
      },
      signIn,
      signOut,
    },
  };
}
