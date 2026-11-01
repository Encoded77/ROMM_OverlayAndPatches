// Moves games saved with EmulatorJS's stock pad map onto RomM's default layout.
import { defineExtension } from "../_core/types";
import { createPlayerControls } from "./controls";

export default defineExtension({ id: "player-controls", player: createPlayerControls });
