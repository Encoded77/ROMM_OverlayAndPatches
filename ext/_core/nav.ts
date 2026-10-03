// Merges extension entries into the v2 navbar list (hook patch 0002).
//
// Upstream's useNavDestinations builds the built-in entries and derives the
// active one from the route path; this takes both and returns them with the
// extensions' entries inserted, so the patch is a two-line wrapper.
import { computed, type ComputedRef } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { extensionNavDestinations } from "./index";

interface BuiltInDestination {
  id: string;
  label: string;
  ariaLabel: string;
  icon: string;
  to: string;
}

export function withExtensionDestinations<
  D extends BuiltInDestination,
  A extends string | null,
>(
  destinations: ComputedRef<D[]>,
  activeId: ComputedRef<A>,
): { destinations: ComputedRef<D[]>; activeId: ComputedRef<A> } {
  const { t } = useI18n();
  const route = useRoute();
  const extra = extensionNavDestinations();
  if (extra.length === 0) return { destinations, activeId };

  const merged = computed(() => {
    const list = [...destinations.value];
    for (const entry of extra) {
      const label = t(entry.labelKey);
      const item = {
        id: `ext:${entry.id}`,
        label,
        ariaLabel: label,
        icon: entry.icon,
        to: entry.to,
      } as unknown as D;
      const at = entry.order ?? list.length;
      list.splice(Math.min(Math.max(at, 0), list.length), 0, item);
    }
    return list;
  });

  const active = computed(() => {
    const hit = extra.find((entry) =>
      route.path.startsWith(entry.activePrefix ?? entry.to),
    );
    return (hit ? `ext:${hit.id}` : activeId.value) as A;
  });

  return { destinations: merged, activeId: active };
}
