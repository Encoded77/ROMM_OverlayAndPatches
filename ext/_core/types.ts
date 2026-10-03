// The contract between the RomM frontend and an overlay extension.
//
// An extension is a folder under src/ext/ with an `extension.ts` whose
// default export is a RommExtension. Everything an extension contributes goes
// through this interface, so the hook patches applied to upstream stay a few
// lines long and never need to know what the extensions do.
import type { App, Ref } from "vue";
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

/** What a player extension gets: the game being played and the app's notices. */
export interface PlayerContext {
  /** RomM's DetailedRom for the running game (user_states, user_saves, id...). */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  rom: Ref<any>;
  /** The app's toast host, the same one RomM's own player notices use. */
  notify: (text: string, options?: { duration?: number; icon?: string }) => void;
}

/** What a player extension may do at each point of a session (all optional). */
export interface PlayerSession {
  /** EmulatorJS reported the game running; its menu bar exists. */
  gameStarted?: () => void;
  /** Quit was clicked; awaited before the player is left. */
  beforeExit?: (kind: "quit") => Promise<void> | void;
  /**
   * Save & Quit captured the machine. Return true to take over storing the
   * state (RomM then does not create its own timestamped one).
   */
  exitState?: (capture: {
    stateFile: Uint8Array | ArrayBuffer;
    screenshotFile?: Uint8Array | ArrayBuffer;
  }) => Promise<boolean> | boolean;
  /** The player is unmounting. */
  dispose?: () => void;
}

/** A player extension: called once per player mount, before EmulatorJS boots. */
export type PlayerExtension = (context: PlayerContext) => PlayerSession;

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
  /** Hooks into the EmulatorJS player (hook patch 0003). */
  player?: PlayerExtension;
  /** Anything else, run once before the router is installed. */
  install?: (context: ExtensionContext) => void | Promise<void>;
}

export function defineExtension(extension: RommExtension): RommExtension {
  return extension;
}
