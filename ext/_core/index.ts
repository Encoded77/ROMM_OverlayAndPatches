// Discovers the overlay extensions and wires the enabled ones into the app.
//
// Called from main.ts (hook patch 0001) before the router is installed, and
// read by the v2 navbar (hook patch 0002). An extension that throws is logged
// and skipped: one broken extension must never take RomM down with it.
import type { App } from "vue";
import type { RouteRecordRaw, Router } from "vue-router";
import { ROUTES } from "@/plugins/routeNames";
import storeConfig from "@/stores/config";
import type { ExtensionNavDestination, RommExtension } from "./types";

const modules = import.meta.glob<{ default: RommExtension }>(
  "../*/extension.ts",
  { eager: true },
);

/** Every extension in the image, on or off (the admin's Extensions tab lists them). */
export const allExtensions: RommExtension[] = Object.values(modules)
  .map((module) => module.default)
  .filter((extension): extension is RommExtension => Boolean(extension?.id))
  .sort((a, b) => a.id.localeCompare(b.id));

/** The extensions this page runs: the enabled ones, fixed at load by installExtensions. */
export const extensions: RommExtension[] = [];

/** What the admin saved (overlay.extensions in config.yml, hook patch 0007). */
export function savedSwitches(): Record<string, boolean> {
  // Not in the generated ConfigResponse type: upstream's schema doesn't know it.
  const config = storeConfig().config as unknown as {
    OVERLAY_EXTENSIONS?: Record<string, boolean>;
  };
  return config.OVERLAY_EXTENSIONS ?? {};
}

/** On when the admin switched it on, or by default when they never chose. */
export function isEnabled(extension: RommExtension): boolean {
  return savedSwitches()[extension.id] ?? extension.defaultEnabled ?? true;
}

/** v1 has no slot for extension views, so it renders the v2 one. */
function withV1Fallback(route: RouteRecordRaw): RouteRecordRaw {
  if (!("components" in route) || !route.components) return route;
  const { v2 } = route.components;
  if (!v2 || route.components.default) return route;
  return { ...route, components: { default: v2, v2 } } as RouteRecordRaw;
}

export async function installExtensions(app: App, router: Router) {
  // main.ts has loaded the config by now; a switch changed later applies on
  // the next page load.
  extensions.splice(0, extensions.length, ...allExtensions.filter(isEnabled));
  for (const extension of extensions) {
    try {
      for (const route of extension.routes ?? []) {
        router.addRoute(ROUTES.MAIN, withV1Fallback(route));
      }
      await extension.install?.({ app, router });
    } catch (error) {
      console.error(`[ext] ${extension.id} failed to install`, error);
    }
  }
}

/** Whether an extension draws its own stream launch for this rom (hook patch 0005). */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function extensionOwnsStream(rom: any): boolean {
  return extensions.some((extension) => extension.ownsStream?.(rom) ?? false);
}

/** Every extension's navbar entries, in declaration order. */
export function extensionNavDestinations(): ExtensionNavDestination[] {
  return extensions.flatMap((extension) => extension.navDestinations ?? []);
}
