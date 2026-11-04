# syntax=docker/dockerfile:1
# Lambda container image. Rocketsled builds this directory with
# --platform linux/amd64 and passes the commit as VERSION.

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -tags lambda.norpc -o /out/bootstrap .

FROM public.ecr.aws/lambda/provided:al2023
COPY --from=build /out/bootstrap ${LAMBDA_RUNTIME_DIR}/bootstrap
CMD ["bootstrap"]
