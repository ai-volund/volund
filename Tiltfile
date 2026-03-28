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
    context='.',
    dockerfile='volund/Dockerfile',
    live_update=[
        sync('volund', '/workspace/volund'),
        sync('volund-proto', '/workspace/volund-proto'),
        run('cd /workspace/volund && go build -o /bin/volund ./cmd/volund',
            trigger=['volund/**/*.go', 'volund-proto/**/*.go']),
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
    context='.',
    dockerfile='volund-agent/Dockerfile',
    live_update=[
        sync('volund-agent', '/workspace/volund-agent'),
        sync('volund-proto', '/workspace/volund-proto'),
        run('cd /workspace/volund-agent && go build -o /bin/volund-agent ./cmd/agent',
            trigger=['volund-agent/**/*.go', 'volund-proto/**/*.go']),
        restart_container(),
    ],
)

# Static agent deployment for local dev (profile: default-orchestrator).
# In production, agent pods are managed by the operator via AgentWarmPool CRs.
k8s_yaml('volund/deploy/local/agent.yaml')
k8s_resource(
    'volund-agent',
    resource_deps=['nats', 'volund'],
    labels=['agent'],
)

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
