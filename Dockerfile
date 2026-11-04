# syntax=docker/dockerfile:1

FROM node:22-alpine AS ui
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/rocketsled ./cmd/rocketsled

# Installs shell out to mass and tofu, so the runtime image carries both plus a
# pre-populated provider cache.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip git \
    && rm -rf /var/lib/apt/lists/*
ARG TOFU_VERSION=1.11.4
ARG MASS_VERSION=2.3.0
RUN ARCH=$(dpkg --print-architecture) \
    && curl -fsSL "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_${ARCH}.zip" -o /tmp/tofu.zip \
    && unzip -q /tmp/tofu.zip tofu -d /usr/local/bin && rm /tmp/tofu.zip \
    && curl -fsSL "https://github.com/massdriver-cloud/mass/releases/download/${MASS_VERSION}/mass_${MASS_VERSION}_linux_${ARCH}.tar.gz" | tar -xz -C /usr/local/bin mass
WORKDIR /app
COPY --from=build /out/rocketsled /usr/local/bin/rocketsled
COPY catalogs ./catalogs
COPY scripts/fetch-providers.sh ./scripts/
RUN scripts/fetch-providers.sh /app/data/plugin-cache
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["rocketsled"]
CMD ["serve", "-config", "/app/rocketsled.yaml"]
