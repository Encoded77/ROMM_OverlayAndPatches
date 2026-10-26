<script setup lang="ts">
// Renders every extension component registered for a named slot (hook patch
// 0004 places <ExtensionSlot name="..." /> in upstream views).
import { computed } from "vue";
import { extensions } from "./index";

const props = defineProps<{ name: string }>();
const components = computed(() =>
  extensions.flatMap((extension) => {
    const component = extension.slots?.[props.name];
    return component ? [{ id: extension.id, component }] : [];
  }),
);
</script>

<template>
  <component :is="entry.component" v-for="entry in components" :key="entry.id" />
</template>
