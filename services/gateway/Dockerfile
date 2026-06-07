# Multi-stage build: compile a static binary, then ship it in a distroless
# image (no shell, no package manager, tiny attack surface, runs non-root).
FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/gateway ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gateway /gateway
COPY --from=build /out/healthcheck /healthcheck
ENTRYPOINT ["/gateway"]
