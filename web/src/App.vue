<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { APIError, type APIClient } from "./api";
import type { AuthPort, AuthUser } from "./auth";
import type {
  APIErrorBody,
  DirectoryStatus,
  EpicView,
  InitiativeView,
  InitiativesView,
  PacketView,
} from "./types";

const props = defineProps<{ auth: AuthPort; api: APIClient }>();
const route = useRoute();
const user = ref<AuthUser | null>(null);
const authReady = ref(false);
const loading = ref(false);
const data = ref<InitiativesView | InitiativeView | EpicView | PacketView | null>(null);
const failure = ref<APIErrorBody | null>(null);

const stopObserving = props.auth.observe((nextUser) => {
  user.value = nextUser;
  authReady.value = true;
});
onBeforeUnmount(stopObserving);

const apiPath = computed(() => {
  switch (route.name) {
    case "initiatives":
      return "/api/initiatives";
    case "initiative":
      return `/api/initiatives/${encodeURIComponent(String(route.params.initiative))}`;
    case "epic":
      return `/api/initiatives/${encodeURIComponent(String(route.params.initiative))}/epics/${encodeURIComponent(String(route.params.epic))}`;
    case "packet":
      return `/api/initiatives/${encodeURIComponent(String(route.params.initiative))}/epics/${encodeURIComponent(String(route.params.epic))}/packets/${encodeURIComponent(String(route.params.packet))}`;
    default:
      return null;
  }
});

watch(
  [user, apiPath],
  async ([currentUser, path]) => {
    data.value = null;
    failure.value = null;
    if (currentUser === null || path === null) return;
    loading.value = true;
    try {
      const token = await currentUser.getToken();
      data.value = await props.api.read(path, token);
    } catch (error) {
      failure.value =
        error instanceof APIError
          ? error.body
          : { code: "read_failed", message: "The read could not be completed." };
    } finally {
      loading.value = false;
    }
  },
  { immediate: true },
);

const directory = computed<DirectoryStatus | undefined>(() => data.value?.directory ?? failure.value?.directory);
const initiatives = computed(() => (route.name === "initiatives" ? (data.value as InitiativesView | null) : null));
const initiative = computed(() => (route.name === "initiative" ? (data.value as InitiativeView | null) : null));
const epic = computed(() => (route.name === "epic" ? (data.value as EpicView | null) : null));
const packet = computed(() => (route.name === "packet" ? (data.value as PacketView | null) : null));

function ageText(seconds: number): string {
  if (seconds < 60) return `${seconds} seconds`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} minutes`;
  return `${Math.floor(seconds / 3600)} hours`;
}

function dateText(value: string): string {
  return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(new Date(value));
}
</script>

<template>
  <a class="skip-link" href="#main">Skip to content</a>
  <header class="site-header">
    <div>
      <p class="eyebrow">Agentic engineering</p>
      <RouterLink class="brand" to="/">Work Tracker</RouterLink>
    </div>
    <button v-if="user" class="secondary" type="button" @click="props.auth.signOut">Sign out</button>
  </header>

  <nav v-if="user && route.name !== 'signed-out'" class="breadcrumbs" aria-label="Breadcrumb">
    <ol>
      <li><RouterLink to="/">Initiatives</RouterLink></li>
      <li v-if="route.params.initiative">
        <RouterLink :to="`/initiatives/${route.params.initiative}`">Initiative {{ route.params.initiative }}</RouterLink>
      </li>
      <li v-if="route.params.epic">
        <RouterLink :to="`/initiatives/${route.params.initiative}/epics/${route.params.epic}`">Epic {{ route.params.epic }}</RouterLink>
      </li>
      <li v-if="route.params.packet" aria-current="page">Packet {{ route.params.packet }}</li>
    </ol>
  </nav>

  <main id="main" tabindex="-1">
    <section v-if="route.name === 'signed-out'" class="empty-state">
      <p class="eyebrow">Session ended</p>
      <h1>You are signed out</h1>
      <p>No tracker data remains on this page.</p>
      <RouterLink class="button-link" to="/">Return to sign in</RouterLink>
    </section>

    <section v-else-if="!authReady" class="empty-state" aria-live="polite">
      <h1>Checking sign-in</h1>
    </section>

    <section v-else-if="!user" class="sign-in">
      <p class="eyebrow">Read-only workspace</p>
      <h1>See the work, without changing it</h1>
      <p>Sign in through Identity Platform. Your signed tenant claim is checked against the latest held tenant directory before any packet is shown.</p>
      <button type="button" @click="props.auth.signIn">Sign in with Google</button>
    </section>

    <template v-else>
      <aside v-if="directory" class="directory-status" :class="{ stale: directory.stale }" :role="directory.stale ? 'alert' : 'status'">
        <strong>Tenant directory {{ directory.stale ? 'stale' : 'verified' }}</strong>
        <span>Published {{ ageText(directory.age_seconds) }} ago; expires {{ dateText(directory.expires_at) }} UTC.</span>
      </aside>

      <section v-if="loading" class="empty-state" aria-live="polite">
        <h1>Loading tracker</h1>
      </section>

      <section v-else-if="failure" class="empty-state" role="alert">
        <p class="eyebrow">{{ failure.code.replaceAll('_', ' ') }}</p>
        <h1>Work is not available</h1>
        <p>{{ failure.message }}</p>
        <p v-if="failure.directory?.stale">The last tenant directory is {{ ageText(failure.directory.age_seconds) }} old. It expired {{ ageText(failure.directory.expired_by_seconds ?? 0) }} ago.</p>
      </section>

      <section v-else-if="initiatives" aria-labelledby="initiatives-heading">
        <p class="eyebrow">Portfolio</p>
        <h1 id="initiatives-heading">Initiatives</h1>
        <p class="lede">Only initiatives belonging to your verified tenant are listed.</p>
        <ul class="card-grid">
          <li v-for="item in initiatives.initiatives" :key="item.id" class="card">
            <h2><RouterLink :to="`/initiatives/${item.id}`">Initiative {{ item.id }}</RouterLink></h2>
            <p>{{ item.epic_count }} epics · {{ item.packet_count }} packets</p>
            <p class="waiting"><span>{{ item.blocked_count }} blocked</span><span>{{ item.unclaimed_count }} unclaimed</span></p>
          </li>
        </ul>
      </section>

      <section v-else-if="initiative" aria-labelledby="initiative-heading">
        <p class="eyebrow">Initiative</p>
        <h1 id="initiative-heading">Initiative {{ initiative.id }}</h1>
        <ul class="card-grid">
          <li v-for="item in initiative.epics" :key="item.id" class="card">
            <h2><RouterLink :to="`/initiatives/${initiative.id}/epics/${item.id}`">Epic {{ item.id }}</RouterLink></h2>
            <p>{{ item.packet_count }} packets</p>
            <p class="waiting"><span>{{ item.blocked_count }} blocked</span><span>{{ item.unclaimed_count }} unclaimed</span></p>
          </li>
        </ul>
      </section>

      <section v-else-if="epic" aria-labelledby="epic-heading">
        <p class="eyebrow">Initiative {{ epic.initiative_id }}</p>
        <h1 id="epic-heading">Epic {{ epic.id }}</h1>
        <div class="table-wrap">
          <table>
            <caption>Packets and visible waiting state</caption>
            <thead><tr><th scope="col">Packet</th><th scope="col">Status</th><th scope="col">Waiting</th></tr></thead>
            <tbody>
              <tr v-for="item in epic.packets" :key="item.id">
                <th scope="row"><RouterLink :to="`/initiatives/${epic.initiative_id}/epics/${epic.id}/packets/${item.id}`">{{ item.id }}</RouterLink></th>
                <td><span class="status-pill">{{ item.status }}</span></td>
                <td>{{ item.blocked ? 'Blocked' : item.unclaimed ? 'Nobody has taken this packet' : '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <article v-else-if="packet" aria-labelledby="packet-heading">
        <header class="packet-header">
          <div><p class="eyebrow">Frozen packet</p><h1 id="packet-heading">{{ packet.packet.id }}</h1></div>
          <span class="status-pill">{{ packet.packet.status }}</span>
        </header>
        <p v-if="packet.packet.status === 'blocked'" class="waiting-callout">Waiting: this packet is blocked.</p>
        <p v-else-if="packet.packet.status === 'not started' && !packet.packet.taken_by" class="waiting-callout">Waiting: nobody has taken this packet.</p>

        <section aria-labelledby="body-heading">
          <h2 id="body-heading">Frozen body</h2>
          <dl class="packet-body">
            <div><dt>Goal</dt><dd>{{ packet.packet.goal }}</dd></div>
            <div><dt>Boundary</dt><dd>{{ packet.packet.boundary }}</dd></div>
            <div><dt>Done when</dt><dd>{{ packet.packet.done_when }}</dd></div>
            <div><dt>Check</dt><dd><code>{{ packet.packet.check }}</code></dd></div>
            <div><dt>Context</dt><dd>{{ packet.packet.context }}</dd></div>
          </dl>
        </section>

        <section aria-labelledby="history-heading">
          <h2 id="history-heading">Full history</h2>
          <ol class="timeline">
            <li v-for="event in packet.packet.history" :key="event.event_id">
              <strong>{{ event.kind }}</strong><span>{{ dateText(event.timestamp) }} UTC · {{ event.actor }}</span>
              <dl v-if="event.body" class="event-facts">
                <div><dt>Goal</dt><dd>{{ event.body.goal }}</dd></div>
                <div><dt>Boundary</dt><dd>{{ event.body.boundary }}</dd></div>
                <div><dt>Done when</dt><dd>{{ event.body.done_when }}</dd></div>
                <div><dt>Check</dt><dd>{{ event.body.check }}</dd></div>
                <div><dt>Context</dt><dd>{{ event.body.context }}</dd></div>
              </dl>
              <p v-if="event.tenant_id">Tenant {{ event.tenant_id }}</p>
              <p v-if="event.parent_id">Supersedes {{ event.parent_id }}</p>
              <p v-if="event.text">{{ event.text }}</p>
              <p v-if="event.from && event.to">{{ event.from }} → {{ event.to }}</p>
              <p v-if="event.evidence?.length">Evidence: {{ event.evidence.join(', ') }}</p>
              <p v-if="event.replacement_id">Replacement {{ event.replacement_id }}</p>
              <p v-if="event.reason">Closed: {{ event.reason }}</p>
            </li>
          </ol>
        </section>

        <section aria-labelledby="comments-heading">
          <h2 id="comments-heading">Comments</h2>
          <p v-if="packet.packet.comments.length === 0">No comments.</p>
          <ol v-else class="timeline">
            <li v-for="comment in packet.packet.comments" :key="comment.event_id">
              <p>{{ comment.text }}</p><span>{{ comment.actor }} · {{ dateText(comment.timestamp) }} UTC</span>
            </li>
          </ol>
        </section>
      </article>

      <section v-else class="empty-state">
        <h1>Page not found</h1>
        <RouterLink to="/">Return to initiatives</RouterLink>
      </section>
    </template>
  </main>

  <footer><p>Read-only · Data is denied when identity or exports cannot be verified.</p></footer>
</template>
