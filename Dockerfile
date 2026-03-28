FROM golang:1.26-bookworm AS builder

# Build from workspace root so the ../volund-proto replace directive resolves.
WORKDIR /workspace

# Copy local proto dependency first — own layer for caching.
COPY volund-proto/ volund-proto/

# Copy module manifests so `go mod download` is cached separately from source.
COPY volund/go.mod volund/go.sum volund/
WORKDIR /workspace/volund
RUN go mod download

# Copy full module source and build.
COPY volund/ .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -ldflags "-s -w" -o /bin/volund ./cmd/volund

# Use slim Debian so Tilt live_update can exec tar/rm for hot reloads.
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /bin/volund /bin/volund
# Profile + skill config for the static profile resolver.
COPY --from=builder /workspace/volund/deploy/local/skills.json /etc/volund/skills.json

EXPOSE 8080 9090 9091

ENTRYPOINT ["/bin/volund"]
CMD ["serve", "--all"]
