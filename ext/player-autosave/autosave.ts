// One "[autosave]" save state per game and core, overwritten on every way out
// of the player: Quit, Save & Quit and closing the tab. Its preview is a
// screenshot cached while playing, so even a tab close gets a recent picture.
//
// EmulatorJS internals used here (4.2.x): window.EJS_emulator.gameManager
// (getState, screenshot), emulator.pause, emulator.callEvent.
import i18n from "@/locales";
import type { PlayerContext, PlayerSession } from "../_core/types";

const MARKER = "[autosave]";
const SHOT_FIRST_MS = 2_000;
const SHOT_EVERY_MS = 10_000;
const SHOT_TIMEOUT_MS = 3_000;

type Bytes = Uint8Array | ArrayBuffer;

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const emulator = (): any => (window as any).EJS_emulator;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const core = (): string => (window as any).EJS_core ?? "";

function csrfToken(): string {
  const match = document.cookie.match(/(?:^|; )romm_csrftoken=([^;]*)/);
  return match ? decodeURIComponent(match[1]) : "";
}

function asBlob(bytes: Bytes): Blob {
  return new Blob([bytes instanceof Uint8Array ? bytes.slice() : bytes]);
}

async function screenshot(): Promise<Uint8Array | null> {
  const manager = emulator()?.gameManager;
  if (!manager?.screenshot) return null;
  try {
    const shot = await Promise.race([
      manager.screenshot(),
      new Promise((_, reject) => setTimeout(reject, SHOT_TIMEOUT_MS)),
    ]);
    return shot?.byteLength ? shot : null;
  } catch {
    // EmulatorJS's screenshot can hang on a busy core; the cache covers it.
    return null;
  }
}

export function createAutosave({ rom, notify }: PlayerContext): PlayerSession {
  let cachedShot: Uint8Array | null = null;
  let shotTimer: ReturnType<typeof setInterval> | undefined;
  let shooting = false;
  let unloading = false;
  let uploading: Promise<void> | null = null;

  function existing() {
    return (rom.value?.user_states ?? []).find(
      (state: { file_name: string; emulator?: string | null }) =>
        state.file_name.includes(MARKER) &&
        (!state.emulator || state.emulator === core()),
    );
  }

  // Started synchronously on purpose: on a tab close, anything behind an
  // await waits for the "Leave site?" prompt and dies with the page.
  function upload(stateFile: Bytes, screenshotFile: Bytes | null): Promise<void> {
    const current = existing();
    const base = `${rom.value.fs_name_no_tags} ${MARKER}`;
    const form = new FormData();
    form.append("stateFile", asBlob(stateFile), current?.file_name ?? `${base}.state`);
    if (screenshotFile) {
      form.append(
        "screenshotFile",
        asBlob(screenshotFile),
        current?.screenshot?.file_name ?? `${base}.png`,
      );
    }
    const url = current
      ? `/api/states/${current.id}`
      : `/api/states?rom_id=${rom.value.id}&emulator=${encodeURIComponent(core())}`;
    return fetch(url, {
      method: current ? "PUT" : "POST",
      body: form,
      credentials: "same-origin",
      headers: { "x-csrftoken": csrfToken() },
    }).then(async (response) => {
      if (!response.ok) throw new Error(`autosave state: HTTP ${response.status}`);
      const stored = await response.json();
      if (current) Object.assign(current, stored);
      else (rom.value.user_states ??= []).unshift(stored);
    });
  }

  function refreshShot() {
    if (shooting) return;
    shooting = true;
    screenshot()
      .then((shot) => {
        if (shot) cachedShot = shot;
      })
      .finally(() => (shooting = false));
  }

  function onBeforeUnload(event: BeforeUnloadEvent) {
    const manager = emulator()?.gameManager;
    if (!manager?.getState) return;
    // EmulatorJS answers this same event by firing its "exit", which tears
    // the core down; dropped below while the prompt is up, so cancelling the
    // close keeps the game running.
    unloading = true;
    setTimeout(() => (unloading = false), 0);
    if (!uploading) {
      const state = manager.getState();
      if (state?.byteLength) {
        uploading = upload(state, cachedShot)
          .catch((error) => console.warn("[ext] autosave on tab close", error))
          .finally(() => (uploading = null));
      }
    }
    // Holds the page (and the upload) behind the browser's leave prompt.
    event.preventDefault();
    event.returnValue = "";
  }
  // Capture phase: runs before EmulatorJS's own listener on the same event.
  window.addEventListener("beforeunload", onBeforeUnload, true);

  async function captureAndUpload(): Promise<void> {
    const emu = emulator();
    const manager = emu?.gameManager;
    if (!manager?.getState) return;
    const shot = (await screenshot()) ?? cachedShot;
    // A running threaded core tears the state it serializes.
    emu.pause?.();
    await new Promise((resolve) => setTimeout(resolve, 50));
    const state = manager.getState();
    if (!state?.byteLength) return;
    await upload(state, shot);
  }

  return {
    gameStarted() {
      const emu = emulator();
      if (emu?.callEvent && !emu.__rommAutosaveWrapped) {
        const callEvent = emu.callEvent.bind(emu);
        emu.callEvent = (name: string, data?: unknown) =>
          name === "exit" && unloading ? 0 : callEvent(name, data);
        emu.__rommAutosaveWrapped = true;
      }
      setTimeout(refreshShot, SHOT_FIRST_MS);
      shotTimer = setInterval(refreshShot, SHOT_EVERY_MS);
    },

    async beforeExit() {
      try {
        await captureAndUpload();
      } catch (error) {
        console.warn("[ext] autosave on quit", error);
        notify(i18n.global.t("ext-player-autosave.upload-failed"), {
          icon: "mdi-sync-alert",
        });
      }
    },

    async exitState({ stateFile, screenshotFile }) {
      try {
        await upload(stateFile, screenshotFile ?? cachedShot);
        return true;
      } catch (error) {
        // RomM stores its own timestamped state instead.
        console.warn("[ext] autosave on save and quit", error);
        return false;
      }
    },

    dispose() {
      clearInterval(shotTimer);
      window.removeEventListener("beforeunload", onBeforeUnload, true);
    },
  };
}
