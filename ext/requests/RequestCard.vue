<script setup lang="ts">
// One request: state, live progress, what to do next, and its extras.
import { RBtn, RProgressLinear, RTag } from "@v2/lib";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { useSnackbar } from "@/v2/composables/useSnackbar";
import {
  type Candidate,
  FINISHED,
  formatBytes,
  formatEta,
  type GameRequest,
  type RequestState,
  requestsApi,
  upsert,
} from "./api";

const props = withDefaults(
  defineProps<{ request: GameRequest; children?: GameRequest[]; showOwner?: boolean; isAdmin?: boolean }>(),
  { children: () => [], showOwner: false, isAdmin: false },
);
const { t, te } = useI18n();
const router = useRouter();
const snackbar = useSnackbar();
const busy = ref<string | null>(null);
const showCandidates = ref(props.request.state === "needs_choice");

type Tone = "neutral" | "info" | "warning" | "success" | "danger" | "brand";
const tone: Record<RequestState, Tone> = {
  requested: "neutral",
  searching: "info",
  needs_choice: "warning",
  wanted: "warning",
  downloading: "brand",
  scanning: "info",
  importing: "info",
  available: "success",
  failed: "danger",
  cancelled: "neutral",
};

const finished = computed(() => FINISHED.includes(props.request.state));
const working = computed(() => ["downloading", "scanning", "importing"].includes(props.request.state));
const progress = computed(() => {
  const l = props.request.live;
  if (!l) return null;
  return {
    percent: Math.round(l.progress * 100),
    speed: l.speed ? `${formatBytes(l.speed)}/s` : "",
    eta: formatEta(l.eta),
    step: stepLabel(l.step),
  };
});
function stepLabel(step?: string): string {
  if (!step) return "";
  const key = `ext-requests.step.${step.split(" ")[0]}`;
  return te(key) ? t(key) : step;
}
// The server sends "code|argument"; the code is translated here, the
// argument is data (release title, indexer names, error message).
const detailText = computed(() => {
  const [code, arg = ""] = (props.request.detail ?? "").split(/\|(.*)/s);
  const key = `ext-requests.detail.${code}`;
  return te(key) ? t(key, { arg }) : props.request.detail;
});
const nextSearch = computed(() =>
  props.request.next_search_at ? new Date(props.request.next_search_at).toLocaleString() : "",
);
const candidates = computed(() => props.request.candidates ?? []);
// The server accepts a choice only in these states (not while searching).
const canChoose = computed(() => ["needs_choice", "wanted", "failed"].includes(props.request.state));

async function act(name: string, run: () => Promise<GameRequest | void>) {
  busy.value = name;
  try {
    const result = await run();
    if (result) upsert(result);
  } catch (error) {
    snackbar.error((error as Error).message);
  } finally {
    busy.value = null;
  }
}

const choose = (c: Candidate) =>
  act(`choose:${c.id}`, () => requestsApi.choose(props.request.id, c.id));
</script>

<template>
  <li class="ext-req">
    <div class="ext-req__main">
      <img v-if="request.cover_url" :src="request.cover_url" alt="" class="ext-req__thumb" loading="lazy" />
      <div v-else class="ext-req__thumb ext-req__thumb--empty" />
      <div class="ext-req__body">
        <div class="ext-req__title">
          <template v-if="request.extra">{{ t(`ext-requests.extra.${request.extra}`) }} · </template>
          {{ request.title }}
          <span class="ext-req__muted">· {{ request.platform_name }}</span>
        </div>
        <div class="ext-req__muted">
          <span v-if="showOwner">{{ request.username }} · </span>
          {{ new Date(request.created_at).toLocaleDateString() }}
          <span v-if="request.detail"> · {{ detailText }}</span>
          <span v-if="request.state === 'wanted' && nextSearch">
            · {{ t("ext-requests.next-search", { at: nextSearch }) }}
          </span>
        </div>
        <div v-if="working" class="ext-req__progress">
          <RProgressLinear
            :model-value="progress?.percent ?? 0"
            :indeterminate="!progress || request.state !== 'downloading'"
            height="6"
            rounded
          />
          <span v-if="progress" class="ext-req__muted">
            <template v-if="request.state === 'downloading'">
              {{ progress.percent }} %<template v-if="progress.speed"> · {{ progress.speed }}</template>
              <template v-if="progress.eta"> · {{ progress.eta }}</template>
            </template>
            <template v-else>{{ progress.step }}</template>
          </span>
        </div>
      </div>
      <RTag :tone="tone[request.state]" :text="t(`ext-requests.state.${request.state}`)" />
      <div class="ext-req__actions">
        <RBtn
          v-if="candidates.length && canChoose"
          size="small"
          variant="text"
          :prepend-icon="showCandidates ? 'mdi-chevron-up' : 'mdi-format-list-bulleted'"
          @click="showCandidates = !showCandidates"
        >
          {{ t("ext-requests.candidates", { n: candidates.length }) }}
        </RBtn>
        <RBtn
          v-if="request.state === 'wanted' || request.state === 'needs_choice'"
          size="small"
          variant="text"
          prepend-icon="mdi-refresh"
          :loading="busy === 'search'"
          @click="act('search', () => requestsApi.searchAgain(request.id))"
        >
          {{ t("ext-requests.search-again") }}
        </RBtn>
        <RBtn
          v-if="request.state === 'available' && request.rom_id"
          size="small"
          variant="flat"
          prepend-icon="mdi-play"
          @click="router.push(`/rom/${request.rom_id}`)"
        >
          {{ t("ext-requests.play") }}
        </RBtn>
        <RBtn
          v-if="request.state === 'failed' || request.state === 'cancelled'"
          size="small"
          variant="text"
          prepend-icon="mdi-restart"
          :loading="busy === 'retry'"
          @click="act('retry', () => requestsApi.retry(request.id))"
        >
          {{ t("ext-requests.retry") }}
        </RBtn>
        <RBtn
          v-if="!finished || request.state === 'failed'"
          size="small"
          variant="text"
          prepend-icon="mdi-close"
          :loading="busy === 'cancel'"
          @click="act('cancel', () => requestsApi.cancel(request.id))"
        >
          {{ t("ext-requests.cancel") }}
        </RBtn>
        <RBtn
          v-if="isAdmin && finished"
          size="small"
          variant="text"
          icon="mdi-delete-outline"
          :aria-label="t('ext-requests.delete')"
          :loading="busy === 'delete'"
          @click="act('delete', () => requestsApi.remove(request.id))"
        />
      </div>
    </div>

    <ul v-if="showCandidates && candidates.length && canChoose" class="ext-req__candidates">
      <li v-for="c in candidates" :key="c.id" class="ext-req__candidate">
        <div class="ext-req__body">
          <div class="ext-req__candidate-title">{{ c.title }}</div>
          <div class="ext-req__muted">
            {{ formatBytes(c.size) }} · {{ c.seeders }} seeders · {{ c.indexer }}
            <template v-if="c.region"> · {{ c.region }}</template>
            <template v-if="c.format"> · {{ c.format }}</template>
            · {{ c.reasons.join(", ") }}
          </div>
        </div>
        <RBtn
          size="small"
          variant="outlined"
          prepend-icon="mdi-download"
          :loading="busy === `choose:${c.id}`"
          @click="choose(c)"
        >
          {{ t("ext-requests.download") }}
        </RBtn>
      </li>
    </ul>

    <ul v-if="children.length" class="ext-req__children">
      <RequestCard
        v-for="child in children"
        :key="child.id"
        :request="child"
        :show-owner="false"
        :is-admin="isAdmin"
      />
    </ul>
  </li>
</template>

<style scoped>
.ext-req {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  border-radius: 12px;
  background: rgba(var(--v-theme-surface), 0.6);
  border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
}
.ext-req__main {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}
.ext-req__thumb {
  width: 40px;
  height: 54px;
  object-fit: cover;
  border-radius: 4px;
  flex-shrink: 0;
}
.ext-req__thumb--empty {
  background: rgba(var(--v-theme-on-surface), 0.08);
}
.ext-req__body {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  flex: 1;
}
.ext-req__title {
  font-weight: 600;
}
.ext-req__muted {
  opacity: 0.7;
  font-size: 0.85rem;
  font-weight: 400;
}
.ext-req__progress {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-width: 420px;
}
.ext-req__actions {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.ext-req__candidates,
.ext-req__children {
  list-style: none;
  margin: 0;
  padding: 0 0 0 52px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ext-req__candidate {
  display: flex;
  gap: 12px;
  align-items: center;
  padding: 6px 8px;
  border-radius: 8px;
  background: rgba(var(--v-theme-on-surface), 0.04);
}
.ext-req__candidate-title {
  font-family: var(--r-font-mono, monospace);
  font-size: 0.85rem;
  word-break: break-all;
}
</style>
