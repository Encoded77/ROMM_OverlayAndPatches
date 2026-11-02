// DS games streamed from the melonDS pool: two launches instead of RomM's single
// Stream button, a private screen per player (linked over DS wireless) and one
// shared screen with four DSes. Play (in the browser) stays as it is.
import { defineAsyncComponent, defineComponent, h, type PropType } from "vue";
import type { SimpleRom } from "@/stores/roms";
import { defineExtension } from "../_core/types";
import { isDsRom } from "./modes";

// Loaded only for a DS game: the buttons pull in the game actions and the
// router, which an eager import would load at app start for every view (and
// into every upstream test that mounts the game page).
const DsModes = defineAsyncComponent(() => import("./DsModes.vue"));

const DsModesSlot = defineComponent({
  props: {
    rom: { type: Object as PropType<SimpleRom>, required: true },
    size: { type: String as PropType<"default" | "large">, required: true },
  },
  setup(props) {
    return () => (isDsRom(props.rom) ? h(DsModes, { rom: props.rom, size: props.size }) : null);
  },
});

export default defineExtension({
  id: "ds-modes",
  slots: { "game.actions": DsModesSlot },
  ownsStream: isDsRom,
});
