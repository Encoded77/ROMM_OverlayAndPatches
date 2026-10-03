// The contract between the RomM frontend and an overlay extension.
//
// An extension is a folder under src/ext/ with an `extension.ts` whose
// default export is a RommExtension. Everything an extension contributes goes
// through this interface, so the hook patches applied to upstream stay a few
// lines long and never need to know what the extensions do.
import type { App } from "vue";
import type { RouteRecordRaw, Router } from "vue-router";

export interface ExtensionNavDestination {
  /** Unique across extensions, e.g. "requests". Prefixed with "ext:" when rendered. */
  id: string;
  /** i18n key, resolved by the navbar with the current locale. */
  labelKey: string;
  /** Material Design Icons name, e.g. "mdi-download". */
  icon: string;
  /** Route path the entry links to. */
  to: string;
  /** Path prefix that lights the entry up; defaults to `to`. */
  activePrefix?: string;
  /** Position among the built-in entries (home=0, platforms=1, ...); end when omitted. */
  order?: number;
}

export interface ExtensionContext {
  app: App;
  router: Router;
}

export interface RommExtension {
  /** Stable identifier, also the folder name. */
  id: string;
  /**
   * Child routes of RomM's main layout. Give each one a `v2` component; the
   * `default` (v1) component falls back to the v2 one when omitted.
   */
  routes?: RouteRecordRaw[];
  /** Entries for the v2 navbar (desktop pill and mobile bottom bar). */
  navDestinations?: ExtensionNavDestination[];
  /** Anything else, run once before the router is installed. */
  install?: (context: ExtensionContext) => void | Promise<void>;
}

export function defineExtension(extension: RommExtension): RommExtension {
  return extension;
}
