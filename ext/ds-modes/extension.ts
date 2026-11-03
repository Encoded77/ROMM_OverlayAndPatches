// DS and 3DS games streamed from an external emulator pool: launches instead of RomM's
// single Stream button, a private screen per player (linked over the console's local
// wireless) and, for the DS, one shared screen with four DSes. Play (in the browser)
// stays as it is.
import { defineAsyncComponent, defineComponent, h, type PropType } from "vue";
import type { SimpleRom } from "@/stores/roms";
import { defineExtension } from "../_core/types";
import { isPoolRom } from "./modes";

// Loaded only for a pool game: the buttons pull in the game actions and the
// router, which an eager import would load at app start for every view (and
// into every upstream test that mounts the game page).
const DsModes = defineAsyncComponent(() => import("./DsModes.vue"));
const StreamMode = defineAsyncComponent(() => import("./StreamMode.vue"));

const DsModesSlot = defineComponent({
  props: {
    rom: { type: Object as PropType<SimpleRom>, required: true },
    size: { type: String as PropType<"default" | "large">, required: true },
  },
  setup(props) {
    return () =>
      isPoolRom(props.rom)
        ? h(DsModes, { rom: props.rom, size: props.size })
        : null;
  },
});

export default defineExtension({
  id: "ds-modes",
  // Needs a stream broker that reads the multiplayer preference as the mode.
  defaultEnabled: false,
  // The launch screen renders "stream.setup" only for a rom this extension owns.
  slots: { "game.actions": DsModesSlot, "stream.setup": StreamMode },
  ownsStream: isPoolRom,
});
