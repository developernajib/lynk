# Multi-stage build: compile static binaries, then ship them in a distroless
# image (no shell, no package manager, tiny attack surface, runs non-root).
FROM golang:1.26 AS build
WORKDIR /src

# Download dependencies first so they cache independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
COPY --from=build /out/worker /worker
COPY --from=build /out/healthcheck /healthcheck
# The worker is the same image started with /worker (compose overrides this).
ENTRYPOINT ["/server"]
