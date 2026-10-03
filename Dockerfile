# RomM with the overlay's frontend: upstream's frontend source at ROMM_VERSION,
# patched (patches/) and extended (ext/), built, and copied over the stock
# image's web root. The backend is upstream's, untouched.
#
# Build through scripts/build.sh, which reads NODE_VERSION from upstream's own
# Dockerfile for the same tag, so the toolchain follows upstream too.

ARG ROMM_VERSION=5.3.1
ARG NODE_VERSION=24.16

FROM node:${NODE_VERSION}-alpine AS frontend
RUN apk add --no-cache git tar
ARG ROMM_VERSION
COPY patches /overlay/patches
COPY ext /overlay/ext
COPY scripts/prepare.sh /overlay/scripts/prepare.sh
RUN sh /overlay/scripts/prepare.sh "${ROMM_VERSION}" /src
WORKDIR /src/frontend
RUN npm ci --ignore-scripts --no-audit --no-fund
# Gates: the patched tree must still typecheck and pass upstream's own tests.
RUN npm run typecheck
RUN npx vitest run --reporter=dot
RUN npm run build

FROM rommapp/romm:${ROMM_VERSION}
# Same order as upstream: the built app, then the static assets on top.
COPY --from=frontend /src/frontend/dist /var/www/html
COPY --from=frontend /src/frontend/assets /var/www/html/assets
LABEL org.opencontainers.image.title="romm-overlay" \
      org.opencontainers.image.description="RomM with overlay patches and extensions"
