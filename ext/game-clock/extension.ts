// "Game clock" button in the EmulatorJS menu bar: day/night or any date, per game.
import { defineExtension } from "../_core/types";
import { createGameClock } from "./clock";

export default defineExtension({ id: "game-clock", player: createGameClock });
