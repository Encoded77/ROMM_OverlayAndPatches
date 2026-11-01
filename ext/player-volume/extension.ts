// Volume slider and Mute for every EmulatorJS core, OpenAL or not.
import { defineExtension } from "../_core/types";
import { createPlayerVolume, installMasterVolume } from "./volume";

export default defineExtension({
  id: "player-volume",
  install: installMasterVolume,
  player: createPlayerVolume,
});
