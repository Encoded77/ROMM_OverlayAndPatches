// Random multiplayer game: a navbar entry that picks one game for two players
// or more, on a platform this user can play (browser core or stream), and
// opens its page, where Play / Stream / the DS modes start it.
import { defineExtension } from "../_core/types";

export default defineExtension({
  id: "random-multi",
  routes: [
    {
      path: "random-multi",
      name: "ext-random-multi",
      meta: { title: "ext-random-multi.title" },
      components: { v2: () => import("./RandomMulti.vue") },
    },
  ],
  navDestinations: [
    {
      id: "random-multi",
      labelKey: "ext-random-multi.title",
      icon: "mdi-dice-multiple-outline",
      to: "/random-multi",
    },
  ],
});
