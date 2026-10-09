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

Cluster commands use the platform API endpoint discovered from `--region` and
`--env` (or their environment variables or config keys). To override the API
endpoint, set `--api-endpoint`, `GCPHCPCTL_API_ENDPOINT`, or the `api_endpoint`
config key.

### Nodepool Management (`nodepool`)

Create, inspect, list, scale, and delete nodepools via the platform API server.

```bash
# Create a nodepool in a cluster
gcphcpctl nodepool create my-nodepool --cluster my-cluster \
  --zone us-central1-a --subnet my-subnet --version 4.22.0 --replicas 2
gcphcpctl nodepool create workers --cluster my-cluster \
  --zone us-central1-a --subnet my-subnet --version 4.22.0 \
  --replicas 3 --instance-type n2-standard-8 --disk-size 200
gcphcpctl nodepool create workers --cluster my-cluster \
  --zone us-central1-a --subnet my-subnet \
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

Nodepool commands use the same endpoint discovery as cluster commands, and the
same `GCPHCPCTL_API_ENDPOINT` / `api_endpoint` escape hatch to bypass it.

### Platform API errors and troubleshooting

Cluster and nodepool commands add operation or resource context to a fixed, safe
error label. For example, command stderr can contain:

```text
Error: listing clusters: not authenticated
Error: listing clusters: permission denied
Error: looking up cluster "missing" in project "my-project": not found
Error: creating nodepool: service unavailable; check the resource before retrying
```

`not authenticated` means the request did not have accepted authentication.
`permission denied` is a generic access-denied label, **not** a diagnosis of a
missing role, binding, or policy rule. Check the selected project and configured
identity source; if access is expected, contact an administrator. ESPv2 may
reject a request before it reaches the Platform API, and not every 401 response
has a representation the CLI can interpret.

Other validation, conflict, not-found, and service errors also use fixed labels.
Normal errors omit HTTP codes, the words `Platform API`, and free-form server
`message`, `details`, and `causes`, including any tokens, authorization headers,
claims, or credential-bearing URLs in those fields. The typed
`platformapi.HTTPError` retains HTTP status and request metadata for callers
and tests. Unsupported response content types, token acquisition failures, and
network failures may instead display `request failed`; a timeout can display
`request timed out`. When the `gcloud` fallback cannot obtain an identity token,
the CLI instead reports `not authenticated; run gcloud auth login`. `-o json`
changes successful result output, not this error contract. Failed commands exit
non-zero.

If a create fails and its outcome is uncertain, inspect the resource before
retrying. For `cluster create --setup-infra`, inspect the cluster **and** the
provisioned IAM/network resources before retrying or cleaning up; repeating
setup blindly may provision another set of resources.

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

For cluster and nodepool commands, set a public GCP region and environment to
discover the platform API endpoint. `cluster create` also discovers the OIDC
issuer. Explicit endpoint values take precedence over discovery, including
values in the config file.

```bash
gcphcpctl regions list --env integration
gcphcpctl cluster list --region us-central1 --env integration
gcphcpctl cluster create my-cluster --region us-central1 --env integration \
  --version 4.22.0-rc.5 --setup-infra --project my-project
```

| Flag | Env Var | Config Key | Description |
|------|---------|------------|-------------|
| `--project` | `GCPHCPCTL_PROJECT` | `project` | GCP project ID |
| `--region` | `GCPHCPCTL_REGION` | `region` | GCP region |
| `--environment` / `--env` | `GCPHCPCTL_ENVIRONMENT` | `environment` / `env` | Environment for endpoint discovery |
| `--output` / `-o` | - | `output` | Output format: `text`, `json`, `yaml` |
| `--api-endpoint` | `GCPHCPCTL_API_ENDPOINT` | `api_endpoint` | Explicit platform API endpoint; overrides discovery |
| `--oidc-endpoint` | `GCPHCPCTL_OIDC_ENDPOINT` | `oidc_endpoint` | Explicit OIDC issuer base URL; overrides discovery |

Config file location: `~/.gcphcpctl/config.yaml`

### Endpoint discovery

Service endpoints are discovered from `--env` and `--region`. `--env` accepts a
known environment name (`integration`, `stage`, `production`) that maps to a
discovery subdomain, or — for a shared dev sector or an ephemeral CI run — the
discovery subdomain itself, e.g.:

```bash
gcphcpctl regions list --env integration
gcphcpctl regions list --env <infra-id>.dev
```

The manifest URL is always `https://discovery.{env}.gcp-hcp.devshift.net/v1/regions.json`,
and every endpoint it advertises must be HTTPS under `gcp-hcp.devshift.net`.

**Precedence:** explicit endpoint flags, environment variables, and config values
win over discovery, in that order. Missing endpoints are discovered from
`--env` and `--region`. If both endpoints are explicit, it does not fetch a
manifest and neither `--env` nor `--region` is required. This keeps existing
scripts using both `--api-endpoint` and `--oidc-endpoint` working.
For an environment without a discovery manifest, supply the endpoints explicitly.

## Project Structure

```
cmd/gcphcpctl/        Entry point for the gcphcpctl binary
pkg/
├── cli/              Root command, version, completion
├── cluster/          Cluster lifecycle commands (create, get, list, delete, login)
├── nodepool/         Nodepool commands (create, get, list, scale, delete)
├── auth/             Authentication and token management
├── platformapi/      Platform API client
├── discovery/        Endpoint discovery manifest client and resolver
├── regions/          List regions advertised by discovery
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
- Google Cloud API commands (`iam`, `network`, `ops`) use Application Default
  Credentials (ADC). For local user credentials, run
  `gcloud auth application-default login`; supported service-account or
  external-account credentials can also be supplied through ADC.
- Platform API commands (`cluster`, `nodepool`) use service-account ADC or
  external-account ADC with service-account impersonation for identity tokens.
  With user ADC or no ADC, they instead use the active gcloud session; run
  `gcloud auth login` if needed. ADC login alone does not establish that session.
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

All commands inherit global `--project`, `--region`, and `--environment` flags
from the root command. Cluster and nodepool commands require either an explicit
platform API endpoint or a discoverable region and environment. Configuration
follows the priority: CLI flags > environment variables > config file.

## Related Repositories

- [gcp-hcp](https://github.com/openshift-online/gcp-hcp) - Design decisions and architecture
- [gcp-hcp-infra](https://github.com/openshift-online/gcp-hcp-infra) - Terraform infrastructure and ArgoCD configuration

### Access management

Access-management commands use the public Platform API and the customer GCP
project selected by `--project`, `GCPHCPCTL_PROJECT`, or configuration as their
namespace. Selecting a project does not grant authority there. Initial
`service-admin` bootstrap is handled by activation/Marketplace, not this CLI.

```bash
# Create a custom permission bundle.
gcphcpctl --project customer-project role create limited-operations \
  --permission cluster.get --permission nodepool.get

# Replace the COMPLETE permission set; omitted permissions are removed.
gcphcpctl --project customer-project role update limited-operations \
  --permission cluster.get --permission nodepool.get --permission nodepool.list

# Grant one user a platform role or a custom role, using independent bindings.
gcphcpctl --project customer-project rolebinding create alice-viewer \
  --subject alice@example.com --platform-role cluster-viewer
gcphcpctl --project customer-project rolebinding create alice-limited \
  --subject alice@example.com --role limited-operations

# Audit grants, including full objects in JSON or YAML.
gcphcpctl --project customer-project rolebinding list --subject alice@example.com
gcphcpctl --project customer-project rolebinding get alice-viewer -o yaml
gcphcpctl --project customer-project role list -o json
gcphcpctl --project customer-project role get limited-operations

# Revoke ONLY this named grant; other bindings remain effective.
gcphcpctl --project customer-project rolebinding delete alice-viewer --confirm
```

Create never overwrites an existing name. Role update uses GET followed by PUT,
retaining resourceVersion and metadata; conflicts are returned without retry.
Bindings continue to reference the updated Role. Deleting a Role with
`role delete <name> --confirm` does not delete referencing bindings, which can
remain unresolved. PlatformRole management and bulk/cross-namespace changes are
not supported. A namespace service-admin may deliberately grant any role,
including `cluster-admin` to themselves; the server enforces authorization.

Email normalization preserves local-part case, normalizes Unicode to NFC, and
lowercases the domain. Subject filtering uses this exact canonical match.

Successful mutations indicate API acceptance, not globally effective access.
Authorization changes may take a moment to propagate globally. The success note
is on stdout for text and stderr for JSON/YAML, keeping structured stdout
parseable. Structured deletion reports kind, name, namespace, and `accepted`,
not a fabricated deleted object or complete principal revocation.

#### Manual access-management acceptance

Use a disposable customer project with a bootstrapped service-admin, a separate
user principal, and a second project for namespace-isolation checks. Confirm the
deployment exposes public `roles`/`rolebindings` routes and enables Cedar before
running these tests. Never delete bootstrap bindings or revoke the sole admin.

1. Create a custom Role and grants as above; inspect list/get in text, JSON and
   YAML. Repeat a create name and verify conflict without overwrite.
2. As the separate viewer, poll a safe read every 5 seconds for up to 2 minutes;
   verify reads become permitted and writes remain denied. Record acceptance and
   observed convergence separately; one replica is not proof of global convergence.
3. Update a custom Role in place and verify omitted permissions eventually stop
   working and new permissions work, without changing the binding identity.
4. Create two independent grants for the principal, delete one by name, and
   verify the other still works. Delete the final relevant grant and observe
   eventual denial using the same bounded polling window.
5. Verify a viewer cannot manage access resources and project-A grants do not
   authorize project-B operations. On disposable resources, verify service-admin
   can deliberately bind cluster-admin to themselves.
6. Submit a stale-resourceVersion Role PUT through an integration harness and
   verify conflict; verify invalid permissions/references produce safe errors.
7. Clean up only test-owned bindings and Roles with confirmed named deletes.

If convergence is not observed within the bounded window, record the timeout
rather than claiming success. Live testing requires explicit test identities and
project selection; unit tests do not use live credentials.
