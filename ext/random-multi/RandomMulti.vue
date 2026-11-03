<script setup lang="ts">
// Picks the game and replaces this route with its page, so Back returns to
// where the entry was clicked. RomM's /roms/random takes no metadata filter,
// so the pick is made here from the list endpoint's id index: two requests,
// uniform over the matching games whatever the library holds.
import { REmptyState, RBtn } from "@v2/lib";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { ROUTES } from "@/plugins/router";
import api from "@/services/api";
import platformApi from "@/services/api/platform";
import storePlatforms from "@/stores/platforms";
import IndexShell from "@/v2/components/shared/IndexShell.vue";
import PageHeader from "@/v2/components/shared/PageHeader.vue";
import { useStreamingStore } from "@/stores/streaming";
import { usePlatformPlayableChecker } from "@/v2/composables/usePlatformPlayable";

const { t } = useI18n();
const router = useRouter();
const platformsStore = storePlatforms();
const streamingStore = useStreamingStore();
const { isPlayable, isStreamable } = usePlatformPlayableChecker();

const state = ref<"loading" | "empty" | "error">("loading");

// Player counts are free text ("1", "1-2", "4+", "2-8"): multiplayer is any
// value whose highest number is above 1.
function isMulti(count: string): boolean {
  return Math.max(0, ...(count.match(/\d+/g) ?? []).map(Number)) > 1;
}

async function pick() {
  state.value = "loading";
  try {
    await streamingStore.fetchConfig();
    const platforms = platformsStore.allPlatforms.length
      ? platformsStore.allPlatforms
      : (await platformApi.getPlatforms()).data;
    const platformIds = platforms
      .filter((p) => p.rom_count > 0 && (isPlayable.value(p.slug) || isStreamable.value(p.slug)))
      .map((p) => p.id);
    if (!platformIds.length) {
      state.value = "empty";
      return;
    }
    // Filter values are computed on the unfiltered library and cached server-side.
    const { data: facets } = await api.get("/roms", {
      params: { limit: 1, with_char_index: false, with_rom_id_index: false, with_filter_values: true },
    });
    const counts = (facets.filter_values?.player_counts ?? []).filter(isMulti);
    if (!counts.length) {
      state.value = "empty";
      return;
    }
    const { data } = await api.get("/roms", {
      params: {
        platform_ids: platformIds,
        player_counts: counts,
        player_counts_logic: "any",
        limit: 1,
        with_char_index: false,
        with_filter_values: false,
        with_rom_id_index: true,
      },
    });
    const ids: number[] = data.rom_id_index ?? [];
    if (!ids.length) {
      state.value = "empty";
      return;
    }
    const id = ids[Math.floor(Math.random() * ids.length)];
    void router.replace({ name: ROUTES.ROM, params: { rom: id } });
  } catch {
    state.value = "error";
  }
}

onMounted(pick);
</script>

<template>
  <IndexShell>
    <template #header>
      <PageHeader :title="t('ext-random-multi.title')" />
    </template>
    <div class="ext-random-multi">
      <REmptyState
        :icon="state === 'loading' ? 'mdi-dice-multiple-outline' : 'mdi-account-multiple-remove-outline'"
        :title="t(`ext-random-multi.${state}`)"
      />
      <RBtn
        v-if="state !== 'loading'"
        variant="outlined"
        prepend-icon="mdi-dice-multiple-outline"
        :text="t('ext-random-multi.retry')"
        @click="pick"
      />
    </div>
  </IndexShell>
</template>

<style scoped>
.ext-random-multi {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 64px 16px;
}
</style>
