// Game requests: ask for a game that is not in the library yet, and follow it
// until it lands. The page is a placeholder until the requests service exists;
// it is here so the pipeline proves an extension reaches the navbar.
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
    {
      id: "requests",
      labelKey: "ext-requests.title",
      icon: "mdi-gift-outline",
      to: "/requests",
    },
  ],
});
