<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { APIError, type APIClient } from "./api";
import type { AuthUser } from "./auth";
import type {
  AgentCredentialIssuedResponse,
  AgentCredentialListResponse,
  AgentCredentialMetadata,
  AgentCredentialMetadataResponse,
  APIErrorBody,
} from "./types";

const props = defineProps<{ api: APIClient; user: AuthUser }>();

const fields = reactive({
  packet_id: "",
  attempt_id: "",
  issuer: "",
  subject: "",
  lifetime_minutes: 60,
});
const credentials = ref<AgentCredentialMetadata[]>([]);
const oneTimeCredential = ref<string | null>(null);
const loading = ref(true);
const busy = ref(false);
const revoking = ref<string | null>(null);
const failure = ref<APIErrorBody | null>(null);
const notice = ref("");
const copyNotice = ref("");

onMounted(loadCredentials);
onBeforeUnmount(() => {
  oneTimeCredential.value = null;
});

async function loadCredentials(): Promise<void> {
  loading.value = true;
  failure.value = null;
  try {
    const token = await props.user.getToken();
    const response = await props.api.read<AgentCredentialListResponse>("/api/agent-credentials", token);
    credentials.value = response.credentials;
  } catch (error) {
    failure.value = errorBody(error, "Agent credentials could not be loaded.");
  } finally {
    loading.value = false;
  }
}

async function createCredential(): Promise<void> {
  failure.value = null;
  notice.value = "";
  copyNotice.value = "";
  if (!validLifetime(fields.lifetime_minutes)) {
    failure.value = expiryFailure();
    return;
  }
  if (!validWorkload(fields.issuer, fields.subject)) {
    failure.value = workloadFailure();
    return;
  }

  busy.value = true;
  try {
    const [token, tenantID] = await Promise.all([props.user.getToken(), props.user.getTenantID()]);
    if (tenantID === null) {
      failure.value = {
        code: "tenant_claim_missing",
        message: "Your sign-in has no tenant claim. Sign out and use an account provisioned for this tracker.",
      };
      return;
    }
    const expiresAt = new Date(Date.now() + fields.lifetime_minutes * 60_000).toISOString();
    const response = await props.api.write<AgentCredentialIssuedResponse>("POST", "/api/agent-credentials", token, {
      packet_id: fields.packet_id,
      attempt_id: fields.attempt_id,
      expires_at: expiresAt,
      workload: { tenant_id: tenantID, issuer: fields.issuer, subject: fields.subject },
    });
    oneTimeCredential.value = response.credential;
    upsert(response.metadata);
    fields.attempt_id = "";
    notice.value = `Credential ${response.metadata.id} created.`;
  } catch (error) {
    failure.value = credentialFailure(error);
  } finally {
    busy.value = false;
  }
}

async function copyCredential(): Promise<void> {
  if (oneTimeCredential.value === null) return;
  try {
    await navigator.clipboard.writeText(oneTimeCredential.value);
    copyNotice.value = "Credential copied.";
  } catch {
    copyNotice.value = "Copy was unavailable. Select the credential and copy it before leaving this page.";
  }
}

async function revokeCredential(id: string): Promise<void> {
  failure.value = null;
  notice.value = "";
  revoking.value = id;
  try {
    const token = await props.user.getToken();
    const response = await props.api.write<AgentCredentialMetadataResponse>(
      "POST",
      `/api/agent-credentials/${encodeURIComponent(id)}/revoke`,
      token,
      {},
    );
    upsert(response.metadata);
    notice.value = `Credential ${id} revoked. Its metadata remains visible.`;
  } catch (error) {
    failure.value = errorBody(error, "The credential could not be revoked.");
  } finally {
    revoking.value = null;
  }
}

function upsert(metadata: AgentCredentialMetadata): void {
  const index = credentials.value.findIndex((credential) => credential.id === metadata.id);
  if (index === -1) {
    credentials.value = [metadata, ...credentials.value];
    return;
  }
  credentials.value = credentials.value.map((credential, position) =>
    position === index ? metadata : credential,
  );
}

function credentialFailure(error: unknown): APIErrorBody {
  if (!(error instanceof APIError)) {
    return { code: "operation_failed", message: "The credential could not be created." };
  }
  switch (error.body.code) {
    case "workload_tenant_mismatch":
      return {
        code: error.body.code,
        message: "That workload belongs to another tenant. Use a workload for your signed-in tenant.",
      };
    case "invalid_workload":
      return workloadFailure();
    case "invalid_credential":
      return expiryFailure();
    default:
      return error.body;
  }
}

function errorBody(error: unknown, fallback: string): APIErrorBody {
  return error instanceof APIError ? error.body : { code: "operation_failed", message: fallback };
}

function workloadFailure(): APIErrorBody {
  return {
    code: "invalid_workload",
    message: "Enter an HTTPS issuer without a query or fragment, and a non-empty workload subject.",
  };
}

function expiryFailure(): APIErrorBody {
  return {
    code: "invalid_credential",
    message: "Choose an expiry from 1 through 60 minutes. Check that the packet ID and attempt are also valid.",
  };
}

function validLifetime(value: number): boolean {
  return Number.isInteger(value) && value >= 1 && value <= 60;
}

function validWorkload(issuer: string, subject: string): boolean {
  if (issuer.trim() !== issuer || subject.trim() !== subject || subject === "" || subject.length > 512) return false;
  try {
    const parsed = new URL(issuer);
    return parsed.protocol === "https:" && parsed.host !== "" && parsed.username === "" && parsed.password === "" &&
      parsed.search === "" && parsed.hash === "" && issuer.length <= 512;
  } catch {
    return false;
  }
}

function dateText(value: string): string {
  const formatted = new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value));
  return `${formatted} UTC`;
}
</script>

<template>
  <section class="credentials" aria-labelledby="credentials-heading">
    <div>
      <p class="eyebrow">Human-issued machine identity</p>
      <h1 id="credentials-heading">Agent credentials</h1>
      <p class="lede">Bind a short-lived credential to one packet attempt and the exact workload that will use it.</p>
    </div>

    <section v-if="oneTimeCredential !== null" class="one-time-credential" aria-labelledby="one-time-heading">
      <h2 id="one-time-heading">Copy this credential now</h2>
      <p><strong>This is the only time it will be shown.</strong> Reloading or leaving this page permanently removes it from the app.</p>
      <code class="credential-value" data-testid="one-time-credential">{{ oneTimeCredential }}</code>
      <p><button type="button" @click="copyCredential">Copy credential</button></p>
      <p v-if="copyNotice" role="status">{{ copyNotice }}</p>
    </section>

    <section aria-labelledby="create-credential-heading">
      <h2 id="create-credential-heading">Create a credential</h2>
      <form class="credential-form" @submit.prevent="createCredential">
        <label for="credential-packet">Packet ID</label>
        <input id="credential-packet" v-model="fields.packet_id" name="packet_id" required autocomplete="off" placeholder="0004-E03-T01" pattern="[0-9]{4}-E[0-9]{2}-T[0-9]{2}" />

        <label for="credential-attempt">Attempt</label>
        <input id="credential-attempt" v-model="fields.attempt_id" name="attempt_id" required autocomplete="off" maxlength="256" />

        <label for="credential-issuer">Workload issuer</label>
        <input id="credential-issuer" v-model="fields.issuer" name="issuer" required autocomplete="off" inputmode="url" placeholder="https://issuer.example" />
        <small>The HTTPS authority that vouches for the workload. Queries and fragments are not accepted.</small>

        <label for="credential-subject">Workload subject</label>
        <input id="credential-subject" v-model="fields.subject" name="subject" required autocomplete="off" maxlength="512" />
        <small>The exact subject the workload presents to the grant lookup.</small>

        <label for="credential-lifetime">Expires after</label>
        <input id="credential-lifetime" v-model.number="fields.lifetime_minutes" name="lifetime_minutes" required type="number" min="1" max="60" step="1" />
        <small>Minutes from now. Credentials may live for at most one hour.</small>

        <p><button type="submit" :disabled="busy">{{ busy ? "Creating…" : "Create credential" }}</button></p>
      </form>
    </section>

    <p v-if="failure" class="error-message" role="alert">
      <strong>{{ failure.code.replaceAll("_", " ") }}</strong>
      <span>{{ failure.message }}</span>
    </p>
    <p v-if="notice" class="success-message" role="status">{{ notice }}</p>

    <section aria-labelledby="credential-list-heading">
      <h2 id="credential-list-heading">Issued credentials</h2>
      <p v-if="loading" aria-live="polite">Loading credentials…</p>
      <p v-else-if="credentials.length === 0" class="empty-state" role="status">No agent credentials have been issued for this tenant.</p>
      <div v-else class="table-wrap">
        <table class="credential-table">
          <caption>Credential metadata and revocation state</caption>
          <thead>
            <tr>
              <th scope="col">ID</th>
              <th scope="col">Workload</th>
              <th scope="col">Packet</th>
              <th scope="col">Attempt</th>
              <th scope="col">Created</th>
              <th scope="col">Expires</th>
              <th scope="col">Last use</th>
              <th scope="col">State</th>
              <th scope="col">Action</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="credential in credentials" :key="credential.id" :data-credential-id="credential.id" :class="{ revoked: credential.revoked_at }">
              <th scope="row"><code>{{ credential.id }}</code></th>
              <td><strong>Issuer</strong> {{ credential.workload.issuer }}<br /><strong>Subject</strong> {{ credential.workload.subject }}</td>
              <td>{{ credential.binding.packet_id }}</td>
              <td>{{ credential.binding.attempt_id }}</td>
              <td>{{ dateText(credential.created_at) }}</td>
              <td>{{ dateText(credential.binding.expires_at) }}</td>
              <td>{{ credential.last_used_at ? dateText(credential.last_used_at) : "Never" }}</td>
              <td>
                <span v-if="credential.revoked_at" class="status-pill revoked-status">Revoked</span>
                <span v-else class="status-pill">Active</span>
                <span v-if="credential.revoked_at"><br />{{ dateText(credential.revoked_at) }}</span>
              </td>
              <td>
                <span v-if="credential.revoked_at">No action</span>
                <button v-else type="button" :disabled="revoking === credential.id" :data-revoke="credential.id" @click="revokeCredential(credential.id)">
                  {{ revoking === credential.id ? "Revoking…" : "Revoke" }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </section>
</template>
