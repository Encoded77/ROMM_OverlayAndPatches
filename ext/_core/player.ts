// Runs every extension's player hooks for one player session (hook patch
// 0003 in views/Player/EmulatorJS/Player.vue). Each hook is isolated: an
// extension that throws is logged and the others, and RomM's own exit path,
// carry on.
import { extensions } from "./index";
import type { PlayerContext, PlayerSession } from "./types";

function guarded<T>(id: string, step: string, run: () => T): T | undefined {
  try {
    return run();
  } catch (error) {
    console.error(`[ext] ${id} ${step} failed`, error);
    return undefined;
  }
}

export function createPlayerSession(context: PlayerContext) {
  const sessions: Array<{ id: string; session: PlayerSession }> = [];
  for (const extension of extensions) {
    if (!extension.player) continue;
    const session = guarded(extension.id, "setup", () =>
      extension.player!(context),
    );
    if (session) sessions.push({ id: extension.id, session });
  }

  return {
    gameStarted() {
      for (const { id, session } of sessions) {
        guarded(id, "gameStarted", () => session.gameStarted?.());
      }
    },
    async beforeExit(kind: "quit") {
      await Promise.all(
        sessions.map(({ id, session }) =>
          Promise.resolve(guarded(id, "beforeExit", () => session.beforeExit?.(kind))).catch(
            (error) => console.error(`[ext] ${id} beforeExit failed`, error),
          ),
        ),
      );
    },
    /** True when an extension stored the Save & Quit state itself. */
    async exitState(capture: Parameters<NonNullable<PlayerSession["exitState"]>>[0]) {
      for (const { id, session } of sessions) {
        if (!session.exitState) continue;
        try {
          if (await session.exitState(capture)) return true;
        } catch (error) {
          console.error(`[ext] ${id} exitState failed`, error);
        }
      }
      return false;
    },
    dispose() {
      for (const { id, session } of sessions) {
        guarded(id, "dispose", () => session.dispose?.());
      }
    },
  };
}
