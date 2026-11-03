<script setup lang="ts">
// Administration > Extensions (hook patch 0008): one switch per overlay
// extension, saved as overlay.extensions in config.yml (hook patch 0007).
// A page runs the set it loaded with, so a change offers a reload.
import { RAlert, RBtn } from "@v2/lib";
import { storeToRefs } from "pinia";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import api from "@/services/api";
import storeConfig from "@/stores/config";
import ConfigFileAlerts from "@/v2/components/Settings/ConfigFileAlerts.vue";
import SettingsSection from "@/v2/components/Settings/SettingsSection.vue";
import SettingsToggleRow from "@/v2/components/Settings/SettingsToggleRow.vue";
import { useSnackbar } from "@/v2/composables/useSnackbar";
import { allExtensions, extensions, isEnabled } from "./index";

const { t, te } = useI18n();
const snackbar = useSnackbar();
const configStore = storeConfig();
const { config } = storeToRefs(configStore);
const saving = ref<string | null>(null);

const rows = computed(() =>
  allExtensions.map((extension) => ({
    id: extension.id,
    enabled: isEnabled(extension),
    running: extensions.includes(extension),
  })),
);
const reloadNeeded = computed(() => rows.value.some((row) => row.enabled !== row.running));

function text(id: string, suffix = ""): string {
  const key = `ext-core.${id}${suffix}`;
  if (te(key)) return t(key);
  return suffix ? "" : id;
}

function reload(): void {
  window.location.reload();
}

async function save(id: string, enabled: boolean): Promise<void> {
  saving.value = id;
  try {
    await api.put(`/config/overlay/extensions/${id}`, { enabled });
    await configStore.fetchConfig();
  } catch (error) {
    console.error(`[ext] could not switch ${id}`, error);
    snackbar.error(t("ext-core.save-failed"));
  } finally {
    saving.value = null;
  }
}
</script>

<template>
  <div class="r-v2-section-stack">
    <ConfigFileAlerts />
    <RAlert v-if="reloadNeeded" type="info">
      {{ t("ext-core.reload-needed") }}
      <template #append>
        <RBtn
          variant="text"
          density="compact"
          prepend-icon="mdi-refresh"
          @click="reload"
        >
          {{ t("ext-core.reload") }}
        </RBtn>
      </template>
    </RAlert>
    <SettingsSection :title="t('ext-core.tab')" icon="mdi-puzzle-outline">
      <p class="r-v2-ext-core__intro">{{ t("ext-core.intro") }}</p>
      <SettingsToggleRow
        v-for="row in rows"
        :key="row.id"
        :model-value="row.enabled"
        :title="text(row.id)"
        :description="text(row.id, '-desc')"
        :disabled="!config.CONFIG_FILE_WRITABLE || saving !== null"
        @update:model-value="(value: boolean) => save(row.id, value)"
      />
    </SettingsSection>
  </div>
</template>

<style scoped>
.r-v2-ext-core__intro {
  margin: 0;
  padding: 12px 16px;
  color: var(--r-color-fg-muted);
}
</style>
