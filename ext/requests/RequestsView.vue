<script setup lang="ts">
import { REmptyState } from "@v2/lib";
import { useI18n } from "vue-i18n";
import IndexShell from "@/v2/components/shared/IndexShell.vue";
import PageHeader from "@/v2/components/shared/PageHeader.vue";

const { t } = useI18n();
</script>

<template>
  <IndexShell>
    <template #header>
      <PageHeader :title="t('ext-requests.title')" />
    </template>
    <REmptyState
      icon="mdi-gift-outline"
      :title="t('ext-requests.empty-title')"
      :hint="t('ext-requests.empty-hint')"
    />
  </IndexShell>
</template>
