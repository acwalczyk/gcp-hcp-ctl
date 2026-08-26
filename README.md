# gcp-hcp-ctl

CLI for managing GCP Hosted Control Plane (HCP) clusters.

Part of the [GCP HCP](https://github.com/openshift-online/gcp-hcp) project. See [design decisions](https://github.com/openshift-online/gcp-hcp/tree/main/design-decisions) for architectural context.

## Quick Start

```bash
# Build
make build

# Configure (project and region are required)
mkdir -p ~/.gcphcpctl
cat > ~/.gcphcpctl/config.yaml << EOF
project: your-gcp-project-id
region: us-central1
EOF

# Or use flags / env vars instead
export GCPHCPCTL_PROJECT=your-gcp-project-id
export GCPHCPCTL_REGION=us-central1
```

## Commands

### Cluster Lifecycle (`cluster`)

Create, inspect, list, delete, and log in to clusters via the platform API server.

```bash
# Create a cluster from pre-provisioned IAM and network configs
gcphcpctl cluster create my-cluster \
  --iam-config-file iam-config.json \
  --network-config-file network-config.json \
  --version 4.22.0-rc.5 --channel-group candidate

# Create a cluster with automatic infrastructure provisioning
gcphcpctl cluster create my-cluster \
  --setup-infra --project my-project \
  --version 4.22.0-rc.5 --channel-group candidate

# Dry run (show payload without creating)
gcphcpctl cluster create my-cluster \
  --iam-config-file iam-config.json \
  --network-config-file network-config.json \
  --dry-run

# Get a cluster by name or ID
gcphcpctl cluster get my-cluster
gcphcpctl cluster get my-cluster -o json

# List all clusters
gcphcpctl cluster list
gcphcpctl cluster list -o json

# Delete a cluster (requires confirmation)
gcphcpctl cluster delete my-cluster --confirm

# Log in to a cluster (configures kubeconfig with gcloud exec auth)
gcphcpctl cluster login my-cluster

# Log in with a custom kubeconfig path
gcphcpctl cluster login my-cluster --kubeconfig ~/.kube/config
```

Cluster commands require `--api-endpoint` (or `GCPHCPCTL_API_ENDPOINT` / `api_endpoint` in config) pointing to the platform API server.

### Nodepool Management (`nodepool`)

Create, inspect, list, scale, and delete nodepools via the platform API server.

```bash
# Create a nodepool in a cluster
gcphcpctl nodepool create my-nodepool --cluster my-cluster --replicas 2
gcphcpctl nodepool create workers --cluster my-cluster \
  --replicas 3 --instance-type n2-standard-8 --disk-size 200
gcphcpctl nodepool create workers --cluster my-cluster \
  --replicas 2 --version 4.22.0-rc.5 --channel-group candidate

# Get a nodepool by name or ID
gcphcpctl nodepool get my-nodepool
gcphcpctl nodepool get my-nodepool -o json

# List all nodepools (or filter by cluster)
gcphcpctl nodepool list
gcphcpctl nodepool list --cluster my-cluster
gcphcpctl nodepool list -o json

# Scale a nodepool
gcphcpctl nodepool scale my-nodepool --replicas 5

# Delete a nodepool (requires confirmation)
gcphcpctl nodepool delete my-nodepool --confirm
```

Nodepool commands require `--api-endpoint` (or `GCPHCPCTL_API_ENDPOINT` / `api_endpoint` in config) pointing to the platform API server.

### IAM Infrastructure for Hosted Clusters (`iam`)

Create and destroy Workload Identity Federation (WIF) infrastructure for
HyperShift clusters, including WIF pools, OIDC providers, and Google Service
Accounts with IAM role bindings.

```bash
# Create IAM infrastructure for a cluster
gcphcpctl iam create <infra-id> --oidc-issuer-url https://oidc.example.com/my-cluster
gcphcpctl iam create <infra-id> --oidc-jwks-file /path/to/jwks.json
gcphcpctl iam create <infra-id> --oidc-issuer-url https://oidc.example.com --output-file iam-output.json

# Destroy IAM infrastructure for a cluster
gcphcpctl iam destroy <infra-id>
gcphcpctl iam destroy <infra-id> --yes    # skip confirmation prompt
```

All create operations are idempotent (safe to run multiple times). Destroy
operations tolerate not-found errors gracefully.

### Network Infrastructure for Hosted Clusters (`network`)

Create and destroy GCP network infrastructure including VPC networks, subnets,
Cloud Routers, Cloud NAT, and firewall rules.

```bash
# Create network infrastructure for a cluster
gcphcpctl network create <infra-id>
gcphcpctl network create <infra-id> --vpc-cidr 10.1.0.0/24
gcphcpctl network create <infra-id> --output-file network-output.json

# Destroy network infrastructure for a cluster
gcphcpctl network destroy <infra-id>
gcphcpctl network destroy <infra-id> --yes    # skip confirmation prompt
```

All create operations are idempotent (safe to run multiple times). Destroy
operations delete resources in reverse dependency order and tolerate not-found
errors gracefully.

### Operational Debugging (`ops`)

Convenience wrappers that run Cloud Workflows to interact with GKE clusters
without direct cluster access (Zero Operator Access).

```bash
# Get resources (kubectl-style)
gcphcpctl ops get pods -n hypershift
gcphcpctl ops get nodes
gcphcpctl ops get deployments -n kube-system
gcphcpctl ops get hc -n clusters               # aliases: hc, hcp, np, deploy, svc, etc.

# Get raw JSON response (full Kubernetes API output)
gcphcpctl ops get pods -n hypershift -o json

# AI-powered pod analysis (uses Vertex AI to diagnose issues from logs/events)
gcphcpctl ops get pods my-pod -n hypershift --analyze

# Pod logs
gcphcpctl ops logs my-pod -n hypershift
gcphcpctl ops logs my-pod -n hypershift -c etcd --tail 50

# Describe resources
gcphcpctl ops describe pods my-pod -n hypershift
gcphcpctl ops describe deployment my-deploy -n kube-system

# Delete resources (pods, jobs, deployments)
gcphcpctl ops delete pods my-pod -n clusters-abc123
gcphcpctl ops delete pods my-pod -n clusters-abc123 --grace-period 0

# Expand PVC storage
gcphcpctl ops expand-volume data-etcd-0 -n clusters-abc123 --size 20Gi

# etcd operations
gcphcpctl ops etcd health -n clusters-abc123
gcphcpctl ops etcd status -n clusters-abc123
gcphcpctl ops etcd member-list -n clusters-abc123
gcphcpctl ops etcd defrag -n clusters-abc123
```

### Workflow Management (`ops wf`)

Direct Cloud Workflow management for arbitrary workflow execution.

```bash
# List deployed workflows
gcphcpctl ops wf list

# List execution history for a workflow
gcphcpctl ops wf list get --limit 5

# Run a workflow
gcphcpctl ops wf run get --data '{"resource_type": "pods", "namespace": "hypershift"}'

# Run async (returns immediately)
gcphcpctl ops wf run describe --data '{"resource_type": "pods", "name": "etcd-0"}' --async

# Check execution status
gcphcpctl ops wf status get <execution-id>

# Resume a paused workflow (callback)
gcphcpctl ops wf resume approval-flow <execution-id> --data '{"approved": true}'
```

## Configuration

Configuration priority: **CLI flags > environment variables > config file**.

| Flag | Env Var | Config Key | Description |
|------|---------|------------|-------------|
| `--project` | `GCPHCPCTL_PROJECT` | `project` | GCP project ID |
| `--region` | `GCPHCPCTL_REGION` | `region` | GCP region |
| `--api-endpoint` | `GCPHCPCTL_API_ENDPOINT` | `api_endpoint` | Platform API endpoint (required for `cluster` commands) |
| `--oidc-endpoint` | `GCPHCPCTL_OIDC_ENDPOINT` | `oidc_endpoint` | OIDC issuer base URL (required for `cluster create`) |
| `--output` / `-o` | - | `output` | Output format: `text`, `json`, `yaml` |

Config file location: `~/.gcphcpctl/config.yaml`

## Project Structure

```
cmd/gcphcpctl/        Entry point for the gcphcpctl binary
pkg/
├── cli/              Root command, version, completion
├── cluster/          Cluster lifecycle commands (create, get, list, delete, login)
├── nodepool/         Nodepool commands (create, get, list, scale, delete)
├── auth/             Authentication and token management
├── platformapi/      Platform API client
├── infra/
│   ├── iam/          IAM infrastructure orchestration and CLI commands
│   └── network/      Network infrastructure orchestration and CLI commands
├── ops/              Operational commands (extractable as plugin)
│   ├── wf/           Workflow management subcommands
│   ├── companion/    AI-powered pod analysis (Vertex AI)
│   └── pam/          Privileged Access Manager commands
├── gcp/
│   ├── iam/          Pure GCP IAM API client wrappers
│   ├── networking/   Pure GCP Compute networking API client wrappers
│   ├── workflows/    Cloud Workflows API client
│   ├── auditlog/     Cloud Audit Log client
│   ├── cloudrun/     Cloud Run API client
│   └── pam/          Privileged Access Manager API client
├── config/           Config file loading
└── output/           Table and JSON output formatting
hack/workflows/       Cloud Workflow YAML definitions
```

## Development

```bash
make build    # Build bin/gcphcpctl
make test     # Run unit tests
make lint     # Run go vet
make clean    # Remove build artifacts
```

### Prerequisites

- Go 1.25+
- GCP credentials: `gcloud auth application-default login`
- Cloud Workflows deployed in the target project/region

## Architecture

The CLI has the following command categories:

- **Cluster lifecycle commands** (`cluster`): Create, inspect, list, delete, and
  login to clusters via the platform API server. The `cluster create` flow supports
  two modes: assembling from pre-provisioned IAM and network config files, or
  automatic infrastructure provisioning via `--setup-infra`. The `cluster login`
  command configures a kubeconfig context with gcloud exec-based authentication
  by resolving the cluster's API endpoint from the platform API status data. Cluster
  lookup supports both name and ID.

- **Nodepool commands** (`nodepool`): Create, inspect, list, scale, and delete
  nodepools within clusters. Nodepools share the same authenticated client setup,
  output formatting (`text`/`json`/`yaml`), and name-or-ID resolution patterns
  as the cluster commands.

- **Infrastructure commands** (`iam`, `network`): Provision and tear down GCP
  resources for HyperShift clusters. These live under `pkg/infra/` for
  orchestration logic and `pkg/gcp/` for pure GCP API client wrappers. The
  separation keeps API clients reusable and side-effect-free while command
  orchestration handles retries, ordering, and user interaction.

- **Operational commands** (`ops`): Debug and remediate running clusters via
  Cloud Workflows (Zero Operator Access pattern). The `ops` subtree is
  self-contained under `pkg/ops/` with no dependencies on `pkg/cli/`, allowing
  extraction into a standalone plugin binary (`gcphcpctl-ops`). A stub entry
  point exists at `cmd/ops/main.go` for when that separation is needed.

All commands inherit global `--project` and `--region` flags from the root
command. Cluster commands additionally require `--api-endpoint`. Configuration
follows the priority: CLI flags > environment variables > config file.

## Related Repositories

- [gcp-hcp](https://github.com/openshift-online/gcp-hcp) - Design decisions and architecture
- [gcp-hcp-infra](https://github.com/openshift-online/gcp-hcp-infra) - Terraform infrastructure and ArgoCD configuration
