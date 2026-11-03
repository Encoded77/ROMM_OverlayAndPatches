<script setup lang="ts">
// The mode on the stream launch screen of a pool game, in place of RomM's
// "Multiplayer mode" switch (patch 0006): that switch is the mode here, and
// under its own name it read as "turn on to play together", which gave the
// shared screen to a player who wanted a private one. The DS picks between
// its two modes by name; the 3DS has only private screens, so the preference
// is forced off (on, RomM would advertise a Join that seats a friend on this
// player's console).
import { RSliderBtnGroup } from "@v2/lib";
import { computed, watchEffect } from "vue";
import { useI18n } from "vue-i18n";
import type { SimpleRom } from "@/stores/roms";
import { useMultiplayerPref } from "@/v2/composables/useMultiplayerPref";
import { hasSharedScreen } from "./modes";

const props = defineProps<{ rom: SimpleRom }>();

const { t } = useI18n();
const { multiplayerOnPlay } = useMultiplayerPref();

const canShare = computed(() => hasSharedScreen(props.rom));
watchEffect(() => {
  if (!canShare.value) multiplayerOnPlay.value = false;
});

const mode = computed({
  get: () => (canShare.value && multiplayerOnPlay.value ? "shared" : "private"),
  set: (value: "private" | "shared") => {
    multiplayerOnPlay.value = value === "shared";
  },
});
const items = computed(() => [
  {
    id: "private" as const,
    label: t("ext-ds-modes.private"),
    icon: "mdi-account-multiple",
  },
  {
    id: "shared" as const,
    label: t("ext-ds-modes.shared"),
    icon: "mdi-view-grid",
  },
]);
</script>

<template>
  <div class="ds-modes__setup">
    <RSliderBtnGroup
      v-if="canShare"
      v-model="mode"
      :items="items"
      :aria-label="t('ext-ds-modes.mode')"
    />
    <span class="ds-modes__hint">
      {{
        t(
          mode === "shared"
            ? "ext-ds-modes.shared-hint"
            : "ext-ds-modes.private-hint",
        )
      }}
    </span>
  </div>
</template>

<style scoped>
/* Same look as the launch screen's own settings (Stream.vue's setup item). */
.ds-modes__setup {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ds-modes__hint {
  font-size: var(--r-font-size-xs);
  color: var(--r-color-fg-muted);
  line-height: 1.35;
}
</style>
