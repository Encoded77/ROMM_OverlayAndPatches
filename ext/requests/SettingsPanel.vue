<script setup lang="ts">
// Admin settings: automatic downloads, provider, re-search schedule,
// notifications, rules per platform, and a live "test a title".
import { RBtn, RSelect, RSwitch, RTag, RTextField } from "@v2/lib";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useSnackbar } from "@/v2/composables/useSnackbar";
import { type Candidate, formatBytes, type Rules, requestsApi, type Settings } from "./api";

const { t } = useI18n();
const snackbar = useSnackbar();
const settings = ref<Settings | null>(null);
const saving = ref(false);
const platform = ref("gba");
const MB = 1024 * 1024;

onMounted(async () => {
  try {
    settings.value = await requestsApi.settings();
  } catch (error) {
    snackbar.error((error as Error).message);
  }
});

const platforms = computed(() =>
  Object.keys(settings.value?.rules ?? {}).filter((p) => p !== "default").sort(),
);
const rules = computed<Rules | null>(() => settings.value?.rules[platform.value] ?? null);

// Lists are edited as comma-separated text.
function listField(key: "regions" | "formats" | "deny") {
  return computed({
    get: () => rules.value?.[key].join(", ") ?? "",
    set: (v: string) => {
      if (rules.value) rules.value[key] = v.split(",").map((x) => x.trim().toLowerCase()).filter(Boolean);
    },
  });
}
const regions = listField("regions");
const formats = listField("formats");
const deny = listField("deny");
function sizeField(key: "min_size" | "max_size" | "set_size") {
  return computed({
    get: () => (rules.value ? String(Math.round((rules.value[key] / MB) * 100) / 100) : ""),
    set: (v: string) => {
      if (rules.value) rules.value[key] = Math.round(Number(v) * MB) || 0;
    },
  });
}
const minSize = sizeField("min_size");
const maxSize = sizeField("max_size");
const setSize = sizeField("set_size");
const minSeeders = computed({
  get: () => String(rules.value?.min_seeders ?? 1),
  set: (v: string) => {
    if (rules.value) rules.value.min_seeders = Number(v) || 0;
  },
});
const schedule = computed({
  get: () => settings.value?.research_minutes.join(", ") ?? "",
  set: (v: string) => {
    if (settings.value)
      settings.value.research_minutes = v.split(",").map((x) => Number(x.trim())).filter((n) => n > 0);
  },
});

async function save() {
  if (!settings.value) return;
  saving.value = true;
  try {
    settings.value = await requestsApi.saveSettings(settings.value);
    snackbar.success(t("ext-requests.settings.saved"));
  } catch (error) {
    snackbar.error((error as Error).message);
  } finally {
    saving.value = false;
  }
}

// Test a title
const testTitle = ref("");
const testing = ref(false);
const results = ref<Candidate[] | null>(null);
const testErrors = ref<string[]>([]);
async function runTest() {
  if (!testTitle.value.trim()) return;
  testing.value = true;
  try {
    const r = await requestsApi.test(testTitle.value.trim(), platform.value);
    results.value = r.results;
    testErrors.value = r.indexer_errors ?? [];
  } catch (error) {
    snackbar.error((error as Error).message);
  } finally {
    testing.value = false;
  }
}
const verdictTone = { auto: "success", candidate: "warning", reject: "danger" } as const;
</script>

<template>
  <div v-if="settings" class="ext-set">
    <section class="ext-set__section">
      <h3>{{ t("ext-requests.settings.general") }}</h3>
      <RSwitch v-model="settings.auto_download" :label="t('ext-requests.settings.auto-download')" />
      <RTextField v-model="schedule" :label="t('ext-requests.settings.schedule')" />
      <div class="ext-set__row">
        <RTextField v-model="settings.ttb.url" :label="t('ext-requests.settings.ttb-url')" />
        <RTextField v-model="settings.ttb.storage" :label="t('ext-requests.settings.ttb-storage')" />
        <RTextField v-model="settings.ttb.engine" :label="t('ext-requests.settings.ttb-engine')" />
      </div>
      <div class="ext-set__row">
        <RSwitch v-model="settings.notify.new_request" :label="t('ext-requests.settings.notify-new')" />
        <RSwitch v-model="settings.notify.needs_choice" :label="t('ext-requests.settings.notify-choice')" />
        <RSwitch v-model="settings.notify.failed" :label="t('ext-requests.settings.notify-failed')" />
        <RSwitch v-model="settings.notify.available" :label="t('ext-requests.settings.notify-available')" />
      </div>
    </section>

    <section class="ext-set__section">
      <h3>{{ t("ext-requests.settings.rules") }}</h3>
      <RSelect v-model="platform" :items="platforms" :label="t('ext-requests.settings.platform')" />
      <template v-if="rules">
        <RTextField v-model="regions" :label="t('ext-requests.settings.regions')" />
        <div class="ext-set__row">
          <RTextField v-model="formats" :label="t('ext-requests.settings.formats')" />
          <RTextField v-model="deny" :label="t('ext-requests.settings.deny')" />
        </div>
        <div class="ext-set__row">
          <RTextField v-model="minSize" type="number" :label="t('ext-requests.settings.min-size')" />
          <RTextField v-model="maxSize" type="number" :label="t('ext-requests.settings.max-size')" />
          <RTextField v-model="setSize" type="number" :label="t('ext-requests.settings.set-size')" />
          <RTextField v-model="minSeeders" type="number" :label="t('ext-requests.settings.min-seeders')" />
        </div>
        <RSelect
          v-model="rules.convert"
          :items="[
            { title: t('ext-requests.settings.convert-none'), value: '' },
            { title: 'CHD', value: 'chd' },
          ]"
          item-title="title"
          item-value="value"
          :label="t('ext-requests.settings.convert')"
        />
      </template>
      <div>
        <RBtn variant="flat" prepend-icon="mdi-content-save" :loading="saving" @click="save">
          {{ t("ext-requests.settings.save") }}
        </RBtn>
      </div>
    </section>

    <section class="ext-set__section">
      <h3>{{ t("ext-requests.settings.test") }}</h3>
      <p class="ext-set__muted">{{ t("ext-requests.settings.test-hint") }}</p>
      <div class="ext-set__row">
        <RTextField v-model="testTitle" :label="t('ext-requests.settings.test-title')" @keyup.enter="runTest" />
        <RBtn variant="outlined" prepend-icon="mdi-flask-outline" :loading="testing" @click="runTest">
          {{ t("ext-requests.settings.test-run") }}
        </RBtn>
      </div>
      <p v-if="testErrors.length" class="ext-set__muted">{{ testErrors.join(" · ") }}</p>
      <p v-if="results && !results.length" class="ext-set__muted">{{ t("ext-requests.no-results") }}</p>
      <ul v-if="results?.length" class="ext-set__results">
        <li v-for="c in results" :key="c.id">
          <RTag :tone="verdictTone[c.verdict]" :text="t(`ext-requests.verdict.${c.verdict}`)" size="small" />
          <span class="ext-set__score">{{ Math.round(c.score) }}</span>
          <div class="ext-set__result-body">
            <div class="ext-set__mono">{{ c.title }}</div>
            <div class="ext-set__muted">
              {{ formatBytes(c.size) }} · {{ c.seeders }} seeders · {{ c.indexer }} · {{ c.reasons.join(", ") }}
            </div>
          </div>
        </li>
      </ul>
    </section>
  </div>
</template>

<style scoped>
.ext-set {
  display: flex;
  flex-direction: column;
  gap: 24px;
}
.ext-set__section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.ext-set__row {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  align-items: center;
}
.ext-set__row > * {
  flex: 1 1 200px;
}
.ext-set__muted {
  opacity: 0.7;
  font-size: 0.85rem;
}
.ext-set__results {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ext-set__results li {
  display: flex;
  gap: 10px;
  align-items: center;
}
.ext-set__score {
  width: 36px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.ext-set__result-body {
  min-width: 0;
}
.ext-set__mono {
  font-family: var(--r-font-mono, monospace);
  font-size: 0.85rem;
  word-break: break-all;
}
</style>
