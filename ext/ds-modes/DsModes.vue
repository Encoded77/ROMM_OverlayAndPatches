<script setup lang="ts">
// The two streamed launches of a DS game. The broker picks the mode from RomM's
// own "open this session to other players" preference, the switch on the stream
// launch screen: off is a private screen whose DS joins the other players' over
// DS wireless, on is one screen of four DSes with an invite link for pads. Each
// button sets that preference before the launch, so private is what a plain
// click on Multiplayer gives, whatever the last session used.
import { useI18n } from "vue-i18n";
import type { SimpleRom } from "@/stores/roms";
import GameActionBtn from "@/v2/components/GameActions/GameActionBtn.vue";
import { useGameActions } from "@/v2/composables/useGameActions";
import { useMultiplayerPref } from "@/v2/composables/useMultiplayerPref";
import { isDsRom } from "./modes";

const props = defineProps<{
  rom: SimpleRom;
  size: "default" | "large";
}>();

const { t } = useI18n();
const actions = useGameActions(() => props.rom);
const { multiplayerOnPlay } = useMultiplayerPref();
</script>

<template>
  <template v-if="isDsRom(rom) && actions.canPlayStream.value">
    <span class="ds-modes__launch" @click.capture="multiplayerOnPlay = false">
      <GameActionBtn
        :rom="rom"
        action="stream"
        :size="size"
        variant="brand"
        with-label
        :label="t('ext-ds-modes.private')"
        icon="mdi-account-multiple"
      />
    </span>
    <span class="ds-modes__launch" @click.capture="multiplayerOnPlay = true">
      <GameActionBtn
        :rom="rom"
        action="stream"
        :size="size"
        variant="surface"
        with-label
        :label="t('ext-ds-modes.shared')"
        icon="mdi-view-grid"
      />
    </span>
  </template>
</template>

<style scoped>
/* The buttons, not their click wrappers, are the row's flex items. */
.ds-modes__launch {
  display: contents;
}
</style>
