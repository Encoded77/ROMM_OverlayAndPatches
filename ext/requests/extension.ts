// Game requests: ask for a game the library lacks and follow it live until
// it lands in RomM. Backend: services/requests (same origin, /ext/requests).
import { defineAsyncComponent } from "vue";
import { defineExtension } from "../_core/types";

export default defineExtension({
  id: "requests",
  routes: [
    {
      path: "requests",
      name: "ext-requests",
      meta: { title: "ext-requests.title" },
      components: { v2: () => import("./RequestsView.vue") },
    },
  ],
  navDestinations: [
    { id: "requests", labelKey: "ext-requests.title", icon: "mdi-gift-outline", to: "/requests" },
  ],
  slots: {
    "search.header": defineAsyncComponent(() => import("./SearchSlot.vue")),
    "game.overview": defineAsyncComponent(() => import("./MissingRow.vue")),
  },
});
