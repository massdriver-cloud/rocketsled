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

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /data
COPY --from=build /out/rocketsled /usr/local/bin/rocketsled
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["rocketsled"]
CMD ["serve", "-config", "/data/rocketsled.yaml"]
