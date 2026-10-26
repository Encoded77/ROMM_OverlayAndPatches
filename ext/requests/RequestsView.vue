<script setup lang="ts">
// The Demandes page: search IGDB for a game the library lacks, request it
// (with extras) on one of the library's platforms, and follow requests live.
import { REmptyState, RTabNav, RTag, RTextField } from "@v2/lib";
import { useDebounceFn } from "@vueuse/core";
import { storeToRefs } from "pinia";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import storeAuth from "@/stores/auth";
import IndexShell from "@/v2/components/shared/IndexShell.vue";
import PageHeader from "@/v2/components/shared/PageHeader.vue";
import { useSnackbar } from "@/v2/composables/useSnackbar";
import { connectLive, FINISHED, type Game, type GameRequest, live, requestsApi, upsert } from "./api";
import GameResult from "./GameResult.vue";
import RequestCard from "./RequestCard.vue";
import SettingsPanel from "./SettingsPanel.vue";

const { t } = useI18n();
const snackbar = useSnackbar();
const { user } = storeToRefs(storeAuth());
const isAdmin = computed(() => user.value?.role === "admin");

type Tab = "mine" | "all" | "history" | "settings";
const tab = ref<Tab>("mine");
const tabs = computed(() => [
  { id: "mine", label: t("ext-requests.tab-mine"), icon: "mdi-account" },
  ...(isAdmin.value ? [{ id: "all", label: t("ext-requests.tab-all"), icon: "mdi-account-group" }] : []),
  { id: "history", label: t("ext-requests.tab-history"), icon: "mdi-history" },
  ...(isAdmin.value ? [{ id: "settings", label: t("ext-requests.tab-settings"), icon: "mdi-cog-outline" }] : []),
]);

const history = ref<GameRequest[]>([]);
const loading = ref(true);
async function refresh() {
  try {
    if (tab.value === "history") {
      history.value = await requestsApi.list(isAdmin.value, true);
    } else if (tab.value !== "settings") {
      (await requestsApi.list(tab.value === "all")).forEach(upsert);
    }
  } catch (error) {
    snackbar.error((error as Error).message);
  } finally {
    loading.value = false;
  }
}
watch(tab, () => {
  loading.value = true;
  refresh();
});
const disconnect = connectLive(refresh);
onBeforeUnmount(disconnect);

// Rows come from the shared live state; extras are listed under their game.
const rows = computed(() => {
  if (tab.value === "history") return history.value.filter((r) => !r.parent_id);
  return Object.values(live.byId)
    .filter((r) => !r.parent_id && (tab.value === "all" || r.user_id === user.value?.id))
    .sort((a, b) => (FINISHED.includes(a.state) === FINISHED.includes(b.state)
      ? b.created_at.localeCompare(a.created_at)
      : FINISHED.includes(a.state) ? 1 : -1));
});
function childrenOf(id: number) {
  const pool = tab.value === "history" ? history.value : Object.values(live.byId);
  return pool.filter((r) => r.parent_id === id);
}

// Search
const query = ref("");
const results = ref<Game[]>([]);
const searching = ref(false);
const runSearch = useDebounceFn(async (q: string) => {
  if (q.trim().length < 2) {
    results.value = [];
    return;
  }
  searching.value = true;
  try {
    results.value = await requestsApi.search(q);
  } catch (error) {
    snackbar.error((error as Error).message);
  } finally {
    searching.value = false;
  }
}, 400);
watch(query, (q) => runSearch(q ?? ""));
</script>

<template>
  <IndexShell>
    <template #header>
      <PageHeader :title="t('ext-requests.title')">
        <RTag
          :tone="live.connected ? 'success' : 'neutral'"
          size="small"
          :prepend-icon="live.connected ? 'mdi-access-point' : 'mdi-access-point-off'"
          :text="live.connected ? t('ext-requests.live') : t('ext-requests.offline')"
        />
      </PageHeader>
    </template>

    <section v-if="tab !== 'settings'" class="ext-requests__search">
      <RTextField
        v-model="query"
        :placeholder="t('ext-requests.search-placeholder')"
        prepend-inner-icon="mdi-magnify"
        autocomplete="off"
      />
      <div v-if="searching" class="ext-requests__hint">{{ t("ext-requests.searching") }}</div>
      <div v-else-if="query.trim().length >= 2 && results.length === 0" class="ext-requests__hint">
        {{ t("ext-requests.no-results") }}
      </div>
      <div v-if="results.length" class="ext-requests__results">
        <GameResult v-for="game in results" :key="game.id" :game="game" />
      </div>
    </section>

    <section class="ext-requests__list">
      <RTabNav
        :model-value="tab"
        :items="tabs"
        variant="underlined"
        @update:model-value="tab = $event as Tab"
      />
      <SettingsPanel v-if="tab === 'settings'" />
      <template v-else>
        <REmptyState
          v-if="!loading && rows.length === 0"
          icon="mdi-gift-outline"
          :title="t('ext-requests.empty-title')"
          :hint="t('ext-requests.empty-hint')"
        />
        <ul v-else class="ext-requests__rows">
          <RequestCard
            v-for="item in rows"
            :key="item.id"
            :request="item"
            :children="childrenOf(item.id)"
            :show-owner="tab !== 'mine'"
            :is-admin="isAdmin"
          />
        </ul>
      </template>
    </section>
  </IndexShell>
</template>

<style scoped>
.ext-requests__search,
.ext-requests__list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 32px;
}
.ext-requests__hint {
  opacity: 0.7;
  font-size: 0.875rem;
}
.ext-requests__results {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 12px;
}
.ext-requests__rows {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
