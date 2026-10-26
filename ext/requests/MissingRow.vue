<script setup lang="ts">
// End of a game page's Overview: games of the same franchise or similar
// ones the library lacks, each requestable.
import { storeToRefs } from "pinia";
import { onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import storeRoms from "@/stores/roms";
import { connectLive, type Game, loadMine, requestsApi } from "./api";
import GameResult from "./GameResult.vue";

const { t } = useI18n();
const { currentRom } = storeToRefs(storeRoms());
const games = ref<Game[]>([]);
const disconnect = connectLive(() => loadMine().catch(() => {}));
onBeforeUnmount(disconnect);

watch(
  () => currentRom.value?.igdb_id,
  async (igdbId) => {
    games.value = [];
    if (!igdbId) return;
    try {
      games.value = (await requestsApi.related(igdbId)).slice(0, 12);
    } catch {
      games.value = [];
    }
  },
  { immediate: true },
);
</script>

<template>
  <section v-if="games.length" class="ext-missing">
    <h3 class="ext-missing__title">{{ t("ext-requests.missing-related") }}</h3>
    <div class="ext-missing__row">
      <GameResult v-for="game in games" :key="game.id" :game="game" compact />
    </div>
  </section>
</template>

<style scoped>
.ext-missing {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 24px;
}
.ext-missing__title {
  font-size: 1rem;
  font-weight: 600;
}
.ext-missing__row {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  padding-bottom: 4px;
}
</style>
