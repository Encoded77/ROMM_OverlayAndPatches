// Per-game clock for the EmulatorJS player: a "Game clock" button in the
// emulator's menu bar sets the time the game sees (Day = today 12:00, Night =
// today 23:00, or any date), remembered per game in localStorage.
//
// The cores read the time through Emscripten, which calls Date.now(), so the
// clock is an offset applied to Date.now for as long as the player is open.
// It is installed when the player is set up, before EmulatorJS boots, so the
// game's first reading of the clock already has it.
import i18n from "@/locales";
import type { PlayerContext, PlayerSession } from "../_core/types";

type Setting = { m: "day" | "night" } | { o: number } | null;

const t = (key: string) => i18n.global.t(`ext-game-clock.${key}`);

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const emulator = (): any => (window as any).EJS_emulator;

// The real clock, kept once per page so a second player never wraps a wrapper.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const realNow: () => number = ((Date as any).__rommRealNow ??= Date.now);

const ICON =
  '<svg role="presentation" focusable="false" viewBox="0 0 24 24"><path d="M12 20a8 8 0 1 0 0-16 8 8 0 0 0 0 16m0-18a10 10 0 1 1 0 20 10 10 0 0 1 0-20m.5 5v5.25l4.5 2.67-.75 1.23L11 13V7z"/></svg>';

export function createGameClock({ rom, notify }: PlayerContext): PlayerSession {
  const key = `romm-clock:${rom.value.id}`;
  let offset = 0;

  function read(): Setting {
    try {
      return JSON.parse(localStorage.getItem(key) ?? "null");
    } catch {
      return null;
    }
  }

  function offsetFor(setting: Setting): number {
    if (!setting) return 0;
    if ("m" in setting) {
      const target = new Date(realNow());
      target.setHours(setting.m === "day" ? 12 : 23, 0, 0, 0);
      return target.getTime() - realNow();
    }
    return Number(setting.o) || 0;
  }

  function describe(): string {
    if (!offset) return t("real-time");
    return new Date(Date.now()).toLocaleString(undefined, {
      weekday: "short",
      day: "numeric",
      month: "short",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  function apply(setting: Setting) {
    if (setting) localStorage.setItem(key, JSON.stringify(setting));
    else localStorage.removeItem(key);
    offset = offsetFor(setting);
    emulator()?.closePopup?.();
    notify(`${t("title")}: ${describe()}`, { icon: "mdi-clock-outline" });
  }

  function open() {
    const emu = emulator();
    if (!emu?.createPopup) return;
    const input = document.createElement("input");
    const now = new Date(Date.now());
    now.setMinutes(now.getMinutes() - now.getTimezoneOffset());
    input.type = "datetime-local";
    input.value = now.toISOString().slice(0, 16);
    input.style.cssText = "color-scheme:dark;font-size:1.1em;padding:4px;margin:8px 0";
    // Keys typed in the field belong to the field, not to the game.
    for (const type of ["keydown", "keyup"]) {
      input.addEventListener(type, (event) => event.stopPropagation());
    }
    const popup = emu.createPopup(t("title"), {
      [t("day")]: () => apply({ m: "day" }),
      [t("night")]: () => apply({ m: "night" }),
      [t("reset")]: () => apply(null),
      [t("apply")]: () => {
        const at = new Date(input.value).getTime();
        if (!Number.isNaN(at)) apply({ o: at - realNow() });
      },
      [t("close")]: () => emu.closePopup(),
    });
    const current = document.createElement("p");
    current.textContent = `${t("title")}: ${describe()}`;
    const hint = document.createElement("p");
    hint.textContent = t("hint");
    hint.style.cssText = "opacity:.7;font-size:.85em;max-width:340px";
    popup.append(current, input, hint);
  }

  offset = offsetFor(read());
  Date.now = () => realNow() + offset;

  return {
    gameStarted() {
      const menu = emulator()?.elements?.menu as HTMLElement | undefined;
      if (!menu || menu.querySelector(".romm-game-clock")) return;
      const button = document.createElement("button");
      button.type = "button";
      button.className = "ejs_menu_button romm-game-clock";
      button.innerHTML = `${ICON}<span class="ejs_menu_text"></span>`;
      button.querySelector("span")!.textContent = t("title");
      button.addEventListener("click", open);
      menu.insertBefore(button, menu.querySelector(".ejs_menu_bar_spacer"));
    },
    dispose() {
      Date.now = realNow;
    },
  };
}
