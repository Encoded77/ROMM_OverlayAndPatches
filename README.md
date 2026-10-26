# RomM overlay and patches

Run [RomM](https://github.com/rommapp/romm) with your own frontend changes and
still take every upstream release.

The image is upstream's `rommapp/romm:<version>` with one thing replaced: the
web app, rebuilt from upstream's frontend source for the same version after
applying a small patch queue and adding extensions. The backend is never
touched.

## How it works

```
upstream frontend @ ROMM_VERSION
  + patches/*.patch        hook points only, a few lines each
  + ext/<name>/            all real code, as new files (never conflicts)
  -> typecheck, upstream unit tests, vite build
  -> COPY dist over rommapp/romm:ROMM_VERSION
  -> browser checks against a throwaway RomM
```

- **Patches** (`patches/`) are git diffs against upstream. Keep them to hook
  points: an import and a call. Today there are four:
  - `0001-ext-install-hook` runs the extensions from `main.ts`, before the
    router is installed.
  - `0002-ext-nav-destinations` lets extensions add entries to the v2 navbar
    (desktop pill and mobile bottom bar).
  - `0003-ext-player-hooks` gives extensions a session in the EmulatorJS
    player (`views/Player/EmulatorJS/Player.vue`, which the v2 player shell
    also uses). The hooks are: set up before EmulatorJS boots, game started,
    Quit, Save & Quit (an extension may store the state itself), and unmount.
  - `0004-ext-view-slots` places `<ExtensionSlot name="..."/>` in two v2
    views: `search.header` (under the search page title) and `game.overview`
    (end of a game page's Overview tab). Slot components read RomM's own
    stores for their context.
- **Extensions** (`ext/<name>/extension.ts`) are found automatically. One
  extension can contribute:
  - routes (children of the main layout, rendered by the v2 shell);
  - navbar entries;
  - locale files (`ext/<name>/locales/<locale>/<namespace>.json`, merged into
    upstream's lazy locale loader, used as `t("<namespace>.key")`);
  - player hooks (`player: (context) => session`, see `ext/_core/types.ts`);
  - an `install()` hook for anything else.

  Extensions today:
  - `requests`: game requests ("Demandes"): a page with IGDB search,
    live request list (SSE) and admin settings, a search fallback slot and a
    "related games missing from the library" slot, all backed by
    `services/requests`.
  - `player-autosave`: one `[autosave]` save state per game and core,
    overwritten on Quit, Save & Quit and tab close. Its preview comes from a
    screenshot cached every 10 s. On a tab close it holds the page behind the
    browser's leave prompt while the upload runs, and ignores EmulatorJS's own
    `exit` teardown, so cancelling the close keeps the game running.
  - `game-clock`: a "Game clock" button in the EmulatorJS menu bar. Day
    (today 12:00), Night (today 23:00), any date, or real time, remembered per
    game. It works by offsetting `Date.now`, which is how the cores read the
    time.

  `ext/_core/` is the framework itself. Extensions are written with RomM's own
  v2 components (`@v2/lib`, `@/v2/components/shared`), so they look native and
  follow theme changes.
- **Toolchain** follows upstream: `scripts/build.sh` reads `NODE_VERSION`
  from upstream's Dockerfile for the tag being built.

## Updating to a new RomM release

Renovate bumps `ARG ROMM_VERSION` in the `Dockerfile` and opens a PR. CI then
runs the full chain:

- **Green:** the PR merges and the image is published as
  `<romm-version>-<run>`.
- **Red:** the PR stays open and the deployed image does not change. The
  failing step says what broke:
  - `PATCH FAILED: <name>`: rebase that patch.
  - a type error or a test failure: an extension uses an API that moved.
  - the browser check: an entry no longer renders.

Rebasing a patch:

```sh
scripts/prepare.sh <new-tag> /tmp/romm        # stops at the failing patch
cd /tmp/romm && git apply --3way --reject ../path/to/patches/NNNN-name.patch
# fix the conflict in the file, then regenerate the patch from the diff
git diff -- frontend/src/<file> > path/to/patches/NNNN-name.patch
```

## Adding an extension

```ts
// ext/hello/extension.ts
import { defineExtension } from "../_core/types";

export default defineExtension({
  id: "hello",
  routes: [{ path: "hello", name: "ext-hello", components: { v2: () => import("./Hello.vue") } }],
  navDestinations: [{ id: "hello", labelKey: "ext-hello.title", icon: "mdi-hand-wave", to: "/hello" }],
});
```

Add `ext/hello/locales/en_US/ext-hello.json` with `{ "title": "Hello" }`, then
add the label to `expected` in `checks/nav-check.mjs`.

## Services

`services/requests/` is the backend of the `requests` extension: a Go service
(SQLite, server-sent events) meant to sit on RomM's origin under
`/ext/requests/` (a reverse-proxy path route), so the browser's RomM session
cookie reaches it and it asks RomM `GET /api/users/me` who the caller is.

- `internal/match`: release scoring against a game, rules per platform
  (regions, formats, sizes, seeders), verdict auto / candidate / reject.
- `internal/provider`: the download provider interface; `provider/ttb`
  implements it on TorrentTrackerBrowser (SSE search, jobs, purge). Another
  provider (Prowlarr + qBittorrent) only has to implement the interface.
- `internal/engine`: the request lifecycle (search, pick or ask, wanted list
  with back-off, download progress, import, link to RomM, extras as child
  requests).
- `internal/importer`: unpack (7z), keep the game files, optional CHD
  (chdman, throttled), place without overwriting, call the scan hook.

Environment: `ROMM_URL`, `ROMM_TOKEN` (roms.read, roms.write, platforms.read),
`IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET`, `TTB_URL`, `TTB_STORAGE` (staging
storage), `STAGING_ROOT` and `LIBRARY_ROOT` (the same filesystem, mounted
once, so placing is a rename), `SCAN_HOOK_URL`, `NTFY_URL`, `NTFY_TOKEN`,
`PUBLIC_URL`, and optionally `LISTEN`, `BASE_PATH`, `DB_PATH`. Everything else
(rules, provider, schedule, notifications) is in the admin settings. CI builds it (vet and tests run in
the image build) and publishes `romm-requests:1.0.<run>` next to the overlay
image.

## Building and checking locally

```sh
scripts/build.sh 5.3.1 romm-overlay   # needs Docker
checks/run.sh romm-overlay:5.3.1      # throwaway RomM + headless Chromium
```

Only the v2 UI (RomM's default since 5.3) gets navbar entries. v1 users can
still open an extension's page by its URL.

## CI

`.forgejo/workflows/build.yml` runs on a Forgejo Actions runner labelled
`romm-overlay` in host mode (it needs Docker). Configure:

| | Name | Purpose |
|---|---|---|
| Variable | `REGISTRY_IMAGE` | e.g. `forgejo.example.com/owner/romm-overlay` |
| Secret | `REGISTRY_USER`, `REGISTRY_TOKEN` | push to that registry |
| Secret | `NTFY_URL`, `NTFY_TOKEN` | optional failure notification |
