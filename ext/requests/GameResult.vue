<script setup lang="ts">
// One IGDB game with a request button per library platform it exists on,
// optional extras, and what is already owned or requested.
import { RBtn, RChip, RTag } from "@v2/lib";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { useSnackbar } from "@/v2/composables/useSnackbar";
import { EXTRAS, type Extra, FINISHED, type Game, live, requestsApi, RequestError, upsert } from "./api";

const props = withDefaults(defineProps<{ game: Game; compact?: boolean }>(), { compact: false });
const { t } = useI18n();
const router = useRouter();
const snackbar = useSnackbar();

const extras = ref<Extra[]>([]);
const showExtras = ref(false);
const busy = ref<string | null>(null);

function openRequest(slug: string) {
  return Object.values(live.byId).find(
    (r) => r.igdb_id === props.game.id && r.platform_slug === slug && !r.parent_id && !FINISHED.includes(r.state),
  );
}

const anyOwned = computed(() => props.game.platforms.some((p) => p.owned_rom_id));

const platforms = computed(() =>
  props.game.platforms.map((p) => ({ ...p, open: openRequest(p.slug) })),
);

function toggle(extra: Extra) {
  extras.value = extras.value.includes(extra)
    ? extras.value.filter((e) => e !== extra)
    : [...extras.value, extra];
}

async function request(slug: string) {
  busy.value = slug;
  try {
    upsert(await requestsApi.create(props.game.id, slug, extras.value));
    snackbar.success(t("ext-requests.requested", { title: props.game.name }));
    extras.value = [];
    showExtras.value = false;
  } catch (error) {
    const e = error as RequestError;
    if (e.status === 409 && e.body?.rom_id) {
      router.push(`/rom/${e.body.rom_id}`);
    } else {
      snackbar.error(e.message);
    }
  } finally {
    busy.value = null;
  }
}
</script>

<template>
  <div class="ext-game" :class="{ 'ext-game--compact': compact }">
    <img v-if="game.cover_url" :src="game.cover_url" :alt="game.name" class="ext-game__cover" loading="lazy" />
    <div v-else class="ext-game__cover ext-game__cover--empty" />
    <div class="ext-game__body">
      <div class="ext-game__title">
        {{ game.name }}
        <span v-if="game.year" class="ext-game__muted">({{ game.year }})</span>
      </div>
      <div class="ext-game__platforms">
        <template v-for="p in platforms" :key="p.slug">
          <span v-if="p.owned_rom_id" class="ext-game__owned">
            <RTag
              tone="success"
              size="small"
              prepend-icon="mdi-check-circle"
              :text="`${p.name} · ${t('ext-requests.in-library')}`"
              class="ext-game__owned-tag"
              role="link"
              tabindex="0"
              @click="router.push(`/rom/${p.owned_rom_id}`)"
              @keyup.enter="router.push(`/rom/${p.owned_rom_id}`)"
            />
            <RBtn
              v-if="extras.length"
              size="small"
              variant="outlined"
              prepend-icon="mdi-puzzle-plus-outline"
              :loading="busy === p.slug"
              @click="request(p.slug)"
            >
              {{ t("ext-requests.request-extras") }}
            </RBtn>
          </span>
          <span v-else class="ext-game__owned">
            <RBtn
              size="small"
              variant="outlined"
              :prepend-icon="p.open ? 'mdi-progress-clock' : 'mdi-gift-outline'"
              :disabled="Boolean(p.open)"
              :loading="busy === p.slug"
              @click="request(p.slug)"
            >
              {{ p.name }}
            </RBtn>
            <RTag
              v-if="p.resembles && !p.open"
              tone="warning"
              size="small"
              prepend-icon="mdi-alert-outline"
              :text="t('ext-requests.resembles', { name: p.resembles.name })"
              :title="t('ext-requests.resembles-hint')"
              class="ext-game__owned-tag ext-game__warn"
              role="link"
              tabindex="0"
              @click="router.push(`/rom/${p.resembles!.rom_id}`)"
              @keyup.enter="router.push(`/rom/${p.resembles!.rom_id}`)"
            />
          </span>
        </template>
        <RBtn
          v-if="!compact"
          size="small"
          variant="text"
          :prepend-icon="showExtras ? 'mdi-chevron-up' : 'mdi-plus'"
          @click="showExtras = !showExtras"
        >
          {{ t("ext-requests.extras") }}<span v-if="extras.length">&nbsp;({{ extras.length }})</span>
        </RBtn>
      </div>
      <div v-if="showExtras && !compact" class="ext-game__extras">
        <span v-if="anyOwned" class="ext-game__muted ext-game__hint">{{ t("ext-requests.extras-owned-hint") }}</span>
        <RChip
          v-for="extra in EXTRAS"
          :key="extra"
          size="small"
          :variant="extras.includes(extra) ? 'flat' : 'outlined'"
          :prepend-icon="extras.includes(extra) ? 'mdi-check' : undefined"
          @click="toggle(extra)"
        >
          {{ t(`ext-requests.extra.${extra}`) }}
        </RChip>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ext-game {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 10px;
  border-radius: 12px;
  background: rgba(var(--v-theme-surface), 0.6);
  border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
}
.ext-game--compact {
  width: 260px;
  flex-shrink: 0;
}
.ext-game__cover {
  width: 64px;
  height: 86px;
  object-fit: cover;
  border-radius: 6px;
  flex-shrink: 0;
}
.ext-game--compact .ext-game__cover {
  width: 48px;
  height: 64px;
}
.ext-game__cover--empty {
  background: rgba(var(--v-theme-on-surface), 0.08);
}
.ext-game__body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  flex: 1;
}
.ext-game__title {
  font-weight: 600;
}
.ext-game__muted {
  opacity: 0.7;
  font-weight: 400;
}
.ext-game__owned {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  max-width: 100%;
}
.ext-game__owned-tag {
  cursor: pointer;
}
/* A ROM name can be long: wrap inside the card rather than overflow it. */
.ext-game__owned .ext-game__warn {
  white-space: normal;
  line-height: 1.3;
}
.ext-game__hint {
  width: 100%;
  font-size: 0.8rem;
}
.ext-game__platforms,
.ext-game__extras {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
</style>
