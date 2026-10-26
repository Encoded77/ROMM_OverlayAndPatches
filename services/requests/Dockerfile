# romm-requests: Go service + the import tools (7z to unpack, chdman for CHD).
# Debian's 7zip opens RAR archives but cannot decompress them: the codec is
# the non-free 7zip-rar package.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go vet ./... && go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /romm-requests ./cmd/romm-requests

FROM debian:trixie-slim
RUN sed -i 's/^Components: main$/Components: main non-free/' /etc/apt/sources.list.d/debian.sources \
 && apt-get update \
 && apt-get install -y --no-install-recommends 7zip 7zip-rar mame-tools ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && command -v 7z && command -v chdman \
 && useradd --uid 1000 --user-group --no-create-home --shell /usr/sbin/nologin app
COPY --from=build /romm-requests /usr/local/bin/romm-requests
USER app
VOLUME /data
EXPOSE 8810
ENTRYPOINT ["romm-requests"]
