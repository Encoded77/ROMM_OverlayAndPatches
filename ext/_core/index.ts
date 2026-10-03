// Discovers the overlay extensions and wires them into the app.
//
// Called from main.ts (hook patch 0001) before the router is installed, and
// read by the v2 navbar (hook patch 0002). An extension that throws is logged
// and skipped: one broken extension must never take RomM down with it.
import type { App } from "vue";
import type { RouteRecordRaw, Router } from "vue-router";
import { ROUTES } from "@/plugins/routeNames";
import type { ExtensionNavDestination, RommExtension } from "./types";

const modules = import.meta.glob<{ default: RommExtension }>(
  "../*/extension.ts",
  { eager: true },
);

export const extensions: RommExtension[] = Object.values(modules)
  .map((module) => module.default)
  .filter((extension): extension is RommExtension => Boolean(extension?.id))
  .sort((a, b) => a.id.localeCompare(b.id));

/** v1 has no slot for extension views, so it renders the v2 one. */
function withV1Fallback(route: RouteRecordRaw): RouteRecordRaw {
  if (!("components" in route) || !route.components) return route;
  const { v2 } = route.components;
  if (!v2 || route.components.default) return route;
  return { ...route, components: { default: v2, v2 } } as RouteRecordRaw;
}

export async function installExtensions(app: App, router: Router) {
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

/** Every extension's navbar entries, in declaration order. */
export function extensionNavDestinations(): ExtensionNavDestination[] {
  return extensions.flatMap((extension) => extension.navDestinations ?? []);
}
