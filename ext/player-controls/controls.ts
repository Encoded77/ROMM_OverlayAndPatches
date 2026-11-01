// Moves games played before the Xbox layout landed onto it.
//
// The default pad layout lives in RomM's config.yml (`emulatorjs.controls`,
// by position on an Xbox pad, players 1 to 4). EmulatorJS, though, saves the
// whole control map into the browser per game the first time it saves any
// setting, and from then on loads that copy instead of the defaults. A game
// played before the layout changed keeps EmulatorJS's stock map: buttons by
// label (PS1 Cross on Xbox B) and nothing at all for players 2 to 4.
//
// When the game starts, a saved map identical to that stock map is replaced
// by the current defaults, once per game and browser. A map the player
// changed differs from the stock one and is left alone.
import type { PlayerSession } from "../_core/types";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const emulator = (): any => (window as any).EJS_emulator;

/** EmulatorJS's stock gamepad map for player 1 (4.2.3 and nightly). */
const STOCK_PAD = [
  "BUTTON_2", "BUTTON_4", "SELECT", "START",
  "DPAD_UP", "DPAD_DOWN", "DPAD_LEFT", "DPAD_RIGHT",
  "BUTTON_1", "BUTTON_3",
  "LEFT_TOP_SHOULDER", "RIGHT_TOP_SHOULDER",
  "LEFT_BOTTOM_SHOULDER", "RIGHT_BOTTOM_SHOULDER",
  "LEFT_STICK", "RIGHT_STICK",
  "LEFT_STICK_X:+1", "LEFT_STICK_X:-1", "LEFT_STICK_Y:+1", "LEFT_STICK_Y:-1",
  "RIGHT_STICK_X:+1", "RIGHT_STICK_X:-1", "RIGHT_STICK_Y:+1", "RIGHT_STICK_Y:-1",
];

type Controls = Record<string, Record<string, { value?: string; value2?: string }>>;

function isStock(controls: Controls): boolean {
  const first = controls["0"] ?? {};
  if (STOCK_PAD.some((label, index) => first[index]?.value2 !== label)) {
    return false;
  }
  return ["1", "2", "3"].every((player) =>
    Object.values(controls[player] ?? {}).every((button) => !button?.value2),
  );
}

export function createPlayerControls(): PlayerSession {
  return {
    gameStarted() {
      const emu = emulator();
      if (!emu?.controls || !emu.defaultControllers || !emu.getLocalStorageKey) {
        return;
      }
      const marker = `romm-controls-layout:${emu.getLocalStorageKey()}`;
      try {
        if (localStorage.getItem(marker)) return;
        localStorage.setItem(marker, "xbox-position");
      } catch {
        return;
      }
      if (!isStock(emu.controls)) return;
      emu.controls = JSON.parse(JSON.stringify(emu.defaultControllers));
      // Nightly keeps this on the frontend, 4.2.3 on the emulator itself.
      if (emu.frontend?.checkGamepadInputs) emu.frontend.checkGamepadInputs();
      else emu.checkGamepadInputs?.();
      emu.saveSettings?.();
    },
  };
}
