import { flushPromises } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { APIError, type APIClient } from "./api";
import { fakeAuth, render, syntheticUser } from "./test-fixtures";
import type { DraftView, PacketRecord } from "./types";

type Fields = {
  packet_id: string;
  target: string;
  goal: string;
  boundary: string;
  done_when: string;
  check: string;
  context: string;
  expected_version?: number;
};

describe("packet authoring", () => {
  it("edits a draft repeatedly, issues it, and supersedes without exposing an issued edit form", async () => {
    const api = authoringAPI();
    const { wrapper } = await render("/initiatives/0004/epics/E02/new", fakeAuth(syntheticUser()).auth, api.client);
    await fillScope(wrapper, "0004-E02-T90", "first");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(wrapper.text()).toContain("version 1 · editable");

    for (const label of ["second", "third", "final"]) {
      await wrapper.get("#goal").setValue(`Goal ${label}`);
      await wrapper.get("form").trigger("submit");
      await flushPromises();
    }
    expect(wrapper.text()).toContain("version 4 · editable");

    await wrapper.get("button.issue").trigger("click");
    await flushPromises();
    expect(wrapper.get("h1").text()).toBe("Packet issued");
    expect(wrapper.text()).toContain("Goal final");
    expect(wrapper.text()).toContain("human-synthetic");
    expect(wrapper.find("#goal").exists()).toBe(false);

    await wrapper.get(".authoring button").trigger("click");
    await flushPromises();
    expect(wrapper.get("h1").text()).toBe("Supersede packet");
    expect(wrapper.text()).toContain("The original remains unchanged");
    await wrapper.get("#packet-id").setValue("0004-E02-T91");
    await wrapper.get("#goal").setValue("Goal corrected");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    await wrapper.get("button.issue").trigger("click");
    await flushPromises();

    expect(wrapper.get("h1").text()).toBe("Packet superseded");
    for (const text of ["0004-E02-T90", "Goal final", "0004-E02-T91", "Goal corrected", "superseded"]) {
      expect(wrapper.text()).toContain(text);
    }
    expect(wrapper.find("#goal").exists()).toBe(false);
    expect(api.write).toHaveBeenCalledTimes(7);
    wrapper.unmount();
  });

  it("shows the server refusal when an incomplete draft is issued", async () => {
    let draft: DraftView | undefined;
    const client: APIClient = {
      read: vi.fn(),
      write: vi.fn(async (_method, path, _token, value) => {
        if (path.endsWith("/drafts")) {
          const fields = value as Fields;
          draft = makeDraft("draft-incomplete", fields, 1);
          return { draft } as never;
        }
        throw new APIError(422, { code: "draft_incomplete", message: "Every packet field is required before issue." });
      }),
    };
    const { wrapper } = await render("/initiatives/0004/epics/E02/new", fakeAuth(syntheticUser()).auth, client);
    await fillScope(wrapper, "0004-E02-T92", "incomplete");
    await wrapper.get("#check").setValue("");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(draft?.check).toBe("");
    await wrapper.get("button.issue").trigger("click");
    await flushPromises();
    expect(wrapper.get("[role=alert]").text()).toContain("Every packet field is required before issue");
    wrapper.unmount();
  });
});

function authoringAPI(): { client: APIClient; write: ReturnType<typeof vi.fn> } {
  let draftNumber = 0;
  let draft: DraftView | undefined;
  let original: PacketRecord | undefined;
  let parentID: string | undefined;
  const write = vi.fn(async (method: "POST" | "PUT", path: string, _token: string, value: unknown) => {
    const fields = structuredClone(value as Fields);
    if (method === "POST" && (path.endsWith("/drafts") || path.endsWith("/supersessions"))) {
      draftNumber += 1;
      parentID = path.endsWith("/supersessions") ? "0004-E02-T90" : undefined;
      draft = makeDraft(`draft-${draftNumber}`, fields, 1, parentID);
      return { draft };
    }
    if (method === "PUT" && draft) {
      draft = makeDraft(draft.id, fields, draft.version + 1, draft.parent_id);
      return { draft };
    }
    if (method === "POST" && path.endsWith("/issue") && draft) {
      draft = { ...draft, state: "issued", version: draft.version + 1 };
      const packet = makePacket(draft, parentID);
      if (parentID && original) {
        const parent = {
          ...original,
          version: original.version + 2,
          superseded_by: packet.id,
          closure: { event_id: "close-parent", timestamp: "2035-05-06T12:00:00Z", actor: "human-synthetic", reason: "superseded" },
        };
        return { draft, packet, parent };
      }
      original = packet;
      return { draft, packet };
    }
    throw new Error(`unexpected write ${method} ${path}`);
  });
  const client: APIClient = {
    read: vi.fn(),
    async write<T>(method: "POST" | "PUT", path: string, token: string, value: unknown): Promise<T> {
      return (await write(method, path, token, value)) as T;
    },
  };
  return { client, write };
}

function makeDraft(id: string, fields: Fields, version: number, parentID?: string): DraftView {
  return {
    id,
    packet_id: fields.packet_id,
    initiative_id: "0004",
    epic_id: "E02",
    target: fields.target,
    tenant_id: "tenant-synthetic",
    parent_id: parentID,
    state: "draft",
    version,
    goal: fields.goal,
    boundary: fields.boundary,
    done_when: fields.done_when,
    check: fields.check,
    context: fields.context,
  };
}

function makePacket(draft: DraftView, parentID?: string): PacketRecord {
  return {
    id: draft.packet_id,
    tenant_id: draft.tenant_id,
    goal: draft.goal,
    boundary: draft.boundary,
    done_when: draft.done_when,
    check: draft.check,
    context: draft.context,
    status: "not started",
    version: 1,
    taken_by: null,
    comments: [],
    evidence: [],
    parent_id: parentID ?? null,
    superseded_by: null,
    closure: null,
    history: [{
      kind: "packet issued",
      event_id: `issue-${draft.packet_id}`,
      timestamp: "2035-05-06T12:00:00Z",
      actor: "human-synthetic",
      tenant_id: draft.tenant_id,
      body: { goal: draft.goal, boundary: draft.boundary, done_when: draft.done_when, check: draft.check, context: draft.context },
      parent_id: parentID,
    }],
  };
}

async function fillScope(wrapper: Awaited<ReturnType<typeof render>>["wrapper"], packetID: string, label: string): Promise<void> {
  await wrapper.get("#packet-id").setValue(packetID);
  await wrapper.get("#goal").setValue(`Goal ${label}`);
  await wrapper.get("#boundary").setValue(`Boundary ${label}`);
  await wrapper.get("#done-when").setValue(`Done ${label}`);
  await wrapper.get("#check").setValue(`Check ${label}`);
  await wrapper.get("#context").setValue(`Context ${label}`);
}
