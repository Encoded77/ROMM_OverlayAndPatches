<script setup lang="ts">
// Under RomM's search title: when the library has (almost) nothing for the
// term, offer the matching IGDB games to request.
import { useDebounceFn } from "@vueuse/core";
import { storeToRefs } from "pinia";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import storeGalleryFilter from "@/stores/galleryFilter";
import storeGalleryRoms from "@/v2/stores/galleryRoms";
import { connectLive, type Game, loadMine, requestsApi } from "./api";
import GameResult from "./GameResult.vue";

const { t } = useI18n();
const { searchTerm } = storeToRefs(storeGalleryFilter());
const { total, initialFetching } = storeToRefs(storeGalleryRoms());
const games = ref<Game[]>([]);
const disconnect = connectLive(() => loadMine().catch(() => {}));
onBeforeUnmount(disconnect);

const term = computed(() => (searchTerm.value ?? "").trim());
const show = computed(() => term.value.length >= 3 && !initialFetching.value && total.value <= 3);

const lookup = useDebounceFn(async () => {
  if (!show.value) {
    games.value = [];
    return;
  }
  try {
    games.value = (await requestsApi.search(term.value))
      .filter((g) => g.platforms.some((p) => !p.owned_rom_id))
      .slice(0, 8);
  } catch {
    games.value = [];
  }
}, 500);
watch([term, show], lookup, { immediate: true });
</script>

<template>
  <section v-if="show && games.length" class="ext-search">
    <div class="ext-search__title">{{ t("ext-requests.not-in-library") }}</div>
    <div class="ext-search__row">
      <GameResult v-for="game in games" :key="game.id" :game="game" compact />
    </div>
  </section>
</template>

<style scoped>
.ext-search {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 12px;
}
.ext-search__title {
  font-weight: 600;
  opacity: 0.85;
}
.ext-search__row {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  padding-bottom: 4px;
}
</style>
