# RomM with the overlay's frontend: upstream's frontend source at ROMM_VERSION,
# patched (patches/) and extended (ext/), built, and copied over the stock
# image's web root. The backend is upstream's except the few files a backend
# hook patch touches (overlay.extensions in config.yml), copied over the stock ones.
#
# Build through scripts/build.sh, which reads NODE_VERSION from upstream's own
# Dockerfile for the same tag, so the toolchain follows upstream too.

ARG ROMM_VERSION=5.4.1
ARG NODE_VERSION=24.16

FROM node:${NODE_VERSION}-alpine AS frontend
RUN apk add --no-cache git tar
ARG ROMM_VERSION
COPY patches /overlay/patches
COPY ext /overlay/ext
COPY scripts/prepare.sh /overlay/scripts/prepare.sh
RUN sh /overlay/scripts/prepare.sh "${ROMM_VERSION}" /src
# The upstream backend files the patches changed, at their paths under /backend.
RUN cd /src && git diff --name-only HEAD -- backend | while read -r f; do \
      mkdir -p "/backend-patched/$(dirname "${f#backend/}")" && cp "$f" "/backend-patched/${f#backend/}"; \
    done && find /backend-patched -type f
WORKDIR /src/frontend
RUN npm ci --ignore-scripts --no-audit --no-fund
# Gates: the patched tree must still typecheck and pass upstream's own tests.
RUN npm run typecheck
# A few upstream tests are timing-sensitive, and Vite's dependency scan can
# restart mid-run and fail a whole file while it loads (no per-test retry
# covers that). So: longer timeout, two retries per test, and one full rerun
# of the suite if it still fails; a real regression fails both runs. Plain
# output so the failure is named in the log.
# Since 5.4 upstream also renders every Storybook story in Chromium (project
# `storybook`); none covers a patched file and this alpine stage has no
# browser, so only the `app` and lint-rule projects run here. The CI's
# browser checks exercise the patched views on a real RomM instead.
RUN NO_COLOR=1 npx vitest run --project app --project eslint-plugin-romm --reporter=dot --retry=2 --testTimeout=20000 \
 || (echo "test suite failed, running it once more" \
     && NO_COLOR=1 npx vitest run --project app --project eslint-plugin-romm --reporter=dot --retry=2 --testTimeout=20000)
RUN npm run build

FROM rommapp/romm:${ROMM_VERSION}
# Same order as upstream: the built app, then the static assets on top.
COPY --from=frontend /src/frontend/dist /var/www/html
COPY --from=frontend /src/frontend/assets /var/www/html/assets
COPY --from=frontend /backend-patched/ /backend/
# Each patched file must still parse with the image's own Python.
RUN --mount=type=bind,from=frontend,source=/backend-patched,target=/patched \
    find /patched -name '*.py' -exec python -c \
      'import ast, sys; [ast.parse(open(f).read(), f) for f in sys.argv[1:]]' {} +
LABEL org.opencontainers.image.title="romm-overlay" \
      org.opencontainers.image.description="RomM with overlay patches and extensions"
