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
  points: an import and a call. Today there are two:
  - `0001-ext-install-hook` runs the extensions from `main.ts`, before the
    router is installed.
  - `0002-ext-nav-destinations` lets extensions add entries to the v2 navbar
    (desktop pill and mobile bottom bar).
- **Extensions** (`ext/<name>/extension.ts`) are found automatically. One
  extension can contribute:
  - routes (children of the main layout, rendered by the v2 shell);
  - navbar entries;
  - locale files (`ext/<name>/locales/<locale>/<namespace>.json`, merged into
    upstream's lazy locale loader, used as `t("<namespace>.key")`);
  - an `install()` hook for anything else.

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
