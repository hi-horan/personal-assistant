# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/assistant ./cmd/assistant

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/assistant /app/assistant

EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/app/assistant"]
CMD ["run", "-c", "/app/config.yaml"]
