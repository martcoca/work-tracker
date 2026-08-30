<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { APIError, type APIClient } from "./api";
import type { AuthUser } from "./auth";
import type { APIErrorBody, DraftResponse, DraftView, IssuedResponse, PacketRecord } from "./types";

const props = defineProps<{
  api: APIClient;
  user: AuthUser;
  initiative: string;
  epic: string;
}>();

const fields = reactive({
  packet_id: "",
  target: "work-tracker",
  goal: "",
  boundary: "",
  done_when: "",
  check: "",
  context: "",
});
const draft = ref<DraftView | null>(null);
const issued = ref<IssuedResponse | null>(null);
const replacementOf = ref<PacketRecord | null>(null);
const failure = ref<APIErrorBody | null>(null);
const notice = ref("");
const busy = ref(false);

const scopePath = computed(
  () => `/api/initiatives/${encodeURIComponent(props.initiative)}/epics/${encodeURIComponent(props.epic)}`,
);

async function saveDraft(): Promise<void> {
  failure.value = null;
  notice.value = "";
  busy.value = true;
  try {
    const token = await props.user.getToken();
    let response: DraftResponse;
    if (draft.value) {
      response = await props.api.write<DraftResponse>("PUT", `/api/drafts/${encodeURIComponent(draft.value.id)}`, token, {
        ...fields,
        expected_version: draft.value.version,
      });
      notice.value = `Draft saved at version ${response.draft.version}.`;
    } else {
      const path = replacementOf.value
        ? `${scopePath.value}/packets/${encodeURIComponent(replacementOf.value.id)}/supersessions`
        : `${scopePath.value}/drafts`;
      response = await props.api.write<DraftResponse>("POST", path, token, { ...fields });
      notice.value = `Draft created at version ${response.draft.version}.`;
    }
    draft.value = response.draft;
  } catch (error) {
    failure.value = errorBody(error);
  } finally {
    busy.value = false;
  }
}

async function issueDraft(): Promise<void> {
  if (!draft.value) return;
  failure.value = null;
  notice.value = "";
  busy.value = true;
  try {
    const token = await props.user.getToken();
    issued.value = await props.api.write<IssuedResponse>(
      "POST",
      `/api/drafts/${encodeURIComponent(draft.value.id)}/issue`,
      token,
      { expected_version: draft.value.version },
    );
    draft.value = issued.value.draft;
    replacementOf.value = null;
    notice.value = `Packet ${issued.value.packet.id} issued. Its scope is now frozen.`;
  } catch (error) {
    failure.value = errorBody(error);
  } finally {
    busy.value = false;
  }
}

function startSupersession(): void {
  if (!issued.value) return;
  replacementOf.value = issued.value.packet;
  fields.packet_id = "";
  fields.target = issued.value.draft.target;
  fields.goal = issued.value.packet.goal;
  fields.boundary = issued.value.packet.boundary;
  fields.done_when = issued.value.packet.done_when;
  fields.check = issued.value.packet.check;
  fields.context = issued.value.packet.context;
  draft.value = null;
  issued.value = null;
  failure.value = null;
  notice.value = "Compose the corrected replacement. The original remains unchanged.";
}

function errorBody(error: unknown): APIErrorBody {
  return error instanceof APIError
    ? error.body
    : { code: "operation_failed", message: "The authoring operation could not be completed." };
}
</script>

<template>
  <section class="authoring" aria-labelledby="authoring-heading">
    <p class="eyebrow">Initiative {{ initiative }} · Epic {{ epic }}</p>

    <template v-if="issued">
      <h1 id="authoring-heading">{{ issued.parent ? "Packet superseded" : "Packet issued" }}</h1>
      <p class="lede">Issued scope is frozen. Corrections require a linked supersession.</p>

      <div v-if="issued.parent" class="supersession-result">
        <article class="card" aria-labelledby="original-heading">
          <p class="eyebrow">Original · closed as {{ issued.parent.closure?.reason }}</p>
          <h2 id="original-heading">{{ issued.parent.id }}</h2>
          <p><strong>Unchanged goal:</strong> {{ issued.parent.goal }}</p>
          <p><strong>Replacement:</strong> {{ issued.parent.superseded_by }}</p>
        </article>
        <article class="card" aria-labelledby="replacement-heading">
          <p class="eyebrow">Replacement</p>
          <h2 id="replacement-heading">{{ issued.packet.id }}</h2>
          <p><strong>Goal:</strong> {{ issued.packet.goal }}</p>
          <p><strong>Parent:</strong> {{ issued.packet.parent_id }}</p>
        </article>
      </div>

      <article v-else class="card issued-result" aria-labelledby="issued-heading">
        <p class="eyebrow">Frozen packet</p>
        <h2 id="issued-heading">{{ issued.packet.id }}</h2>
        <dl class="packet-body">
          <div><dt>Goal</dt><dd>{{ issued.packet.goal }}</dd></div>
          <div><dt>Boundary</dt><dd>{{ issued.packet.boundary }}</dd></div>
          <div><dt>Done when</dt><dd>{{ issued.packet.done_when }}</dd></div>
          <div><dt>Check</dt><dd>{{ issued.packet.check }}</dd></div>
          <div><dt>Context</dt><dd>{{ issued.packet.context }}</dd></div>
          <div><dt>Issued by</dt><dd>{{ issued.packet.history[0]?.actor }}</dd></div>
        </dl>
      </article>

      <button type="button" @click="startSupersession">Create supersession</button>
    </template>

    <template v-else>
      <h1 id="authoring-heading">{{ replacementOf ? "Supersede packet" : "Author a packet" }}</h1>
      <p v-if="replacementOf" class="lede">
        Replacing {{ replacementOf.id }}. Its frozen body will remain recoverable and linked to the replacement.
      </p>
      <p v-else class="lede">Drafts are freely editable. Issuing freezes every scope field permanently.</p>

      <form class="authoring-form" @submit.prevent="saveDraft">
        <label for="packet-id">Packet ID</label>
        <input id="packet-id" v-model="fields.packet_id" name="packet_id" required autocomplete="off" placeholder="0004-E02-T03" />

        <label for="target">Target repository</label>
        <input id="target" v-model="fields.target" name="target" required autocomplete="off" />

        <label for="goal">Goal</label>
        <textarea id="goal" v-model="fields.goal" name="goal" required rows="3" />

        <label for="boundary">Boundary</label>
        <textarea id="boundary" v-model="fields.boundary" name="boundary" required rows="4" />

        <label for="done-when">Done when</label>
        <textarea id="done-when" v-model="fields.done_when" name="done_when" required rows="3" />

        <label for="check">Check</label>
        <textarea id="check" v-model="fields.check" name="check" required rows="3" />

        <label for="context">Context</label>
        <textarea id="context" v-model="fields.context" name="context" required rows="4" />

        <div class="authoring-actions">
          <button type="submit" :disabled="busy">{{ draft ? "Save changes" : replacementOf ? "Create replacement draft" : "Create draft" }}</button>
          <button v-if="draft" class="issue" type="button" :disabled="busy" @click="issueDraft">Issue and freeze</button>
        </div>
      </form>

      <p v-if="draft" class="draft-status" role="status">Draft {{ draft.id }} · version {{ draft.version }} · editable</p>
    </template>

    <p v-if="notice" class="success-message" role="status">{{ notice }}</p>
    <div v-if="failure" class="error-message" role="alert">
      <strong>{{ failure.code.replaceAll("_", " ") }}</strong>
      <span>{{ failure.message }}</span>
    </div>
  </section>
</template>
