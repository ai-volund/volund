# Volund local development Tiltfile
# Requires: Docker Desktop Kubernetes, Tilt, kubectl
# Run from the workspace root (VOLUND/) with: tilt up
#
# Usage:
#   tilt up          — start all services
#   tilt up volund   — start only the platform core
#   tilt down        — stop everything

# ── Infrastructure ────────────────────────────────────────────────────────────

k8s_yaml('volund/deploy/local/postgres.yaml')
k8s_yaml('volund/deploy/local/nats.yaml')

k8s_resource('postgres', port_forwards=['5432:5432'], labels=['infra'])
k8s_resource('nats',     port_forwards=['4222:4222', '8222:8222'], labels=['infra'])

# ── volund (platform core) ────────────────────────────────────────────────────

docker_build(
    'ghcr.io/ai-volund/volund',
    context='volund',
    dockerfile='volund/Dockerfile',
    live_update=[
        sync('volund', '/src'),
        run('cd /src && go build -o /bin/volund ./cmd/volund', trigger=['volund/**/*.go']),
        restart_container(),
    ],
)

k8s_yaml('volund/deploy/local/volund.yaml')
k8s_resource(
    'volund',
    port_forwards=['8080:8080', '9090:9090', '9091:9091'],
    resource_deps=['postgres', 'nats'],
    labels=['platform'],
)

# ── volund-agent ──────────────────────────────────────────────────────────────

docker_build(
    'ghcr.io/ai-volund/volund-agent',
    context='volund-agent',
    dockerfile='volund-agent/Dockerfile',
    live_update=[
        sync('volund-agent', '/src'),
        run('cd /src && go build -o /bin/volund-agent ./cmd/agent', trigger=['volund-agent/**/*.go']),
        restart_container(),
    ],
)

# Agent pods are managed by the operator; no static k8s_yaml here.

# ── volund-operator ───────────────────────────────────────────────────────────

docker_build(
    'ghcr.io/ai-volund/volund-operator',
    context='volund-operator',
    dockerfile='volund-operator/Dockerfile',
    live_update=[
        sync('volund-operator', '/src'),
        run('cd /src && go build -o /bin/volund-operator ./cmd/operator', trigger=['volund-operator/**/*.go']),
        restart_container(),
    ],
)

k8s_yaml('volund/deploy/local/operator.yaml')
k8s_resource(
    'volund-operator',
    resource_deps=['volund'],
    labels=['platform'],
)

# ── CRDs ──────────────────────────────────────────────────────────────────────

k8s_yaml('volund/deploy/crds/agentwarmpool.yaml')
k8s_yaml('volund/deploy/crds/agentinstance.yaml')

# ── Sample warm pool for local dev ────────────────────────────────────────────

k8s_yaml('volund/deploy/local/warmpool.yaml')
