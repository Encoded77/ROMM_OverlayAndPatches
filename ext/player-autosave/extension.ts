// Keeps one "[autosave]" save state per game up to date on Quit, Save & Quit
// and tab close.
import { defineExtension } from "../_core/types";
import { createAutosave } from "./autosave";

export default defineExtension({ id: "player-autosave", player: createAutosave });
