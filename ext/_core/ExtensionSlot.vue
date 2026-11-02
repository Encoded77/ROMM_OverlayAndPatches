<script setup lang="ts">
// Renders every extension component registered for a named slot (hook patches
// 0004 and 0005 place <ExtensionSlot name="..." /> in upstream views). Any
// other attribute the view passes (the rom, a size) goes to each component.
import { computed } from "vue";
import { extensions } from "./index";

defineOptions({ inheritAttrs: false });

const props = defineProps<{ name: string }>();
const components = computed(() =>
  extensions.flatMap((extension) => {
    const component = extension.slots?.[props.name];
    return component ? [{ id: extension.id, component }] : [];
  }),
);
</script>

<template>
  <component
    :is="entry.component"
    v-for="entry in components"
    :key="entry.id"
    v-bind="$attrs"
  />
</template>
