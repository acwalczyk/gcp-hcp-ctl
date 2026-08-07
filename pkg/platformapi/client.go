// Package platformapi provides a typed REST client for the platform-api-server,
// which serves Kubernetes-style resources under the gcp.managed.openshift.io API group.
package platformapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/auth"

	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

func init() {
	if err := gcpv1.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("registering platform-api types: %v", err))
	}
}

// Client wraps a rest.RESTClient for the platform-api-server.
// The project (GCP project ID) is stored once at construction and used as
// the Kubernetes namespace for all scoped operations.
type Client struct {
	restClient rest.Interface
	project    string
}

// NewClient creates a platform-api client from an API endpoint URL, project ID, and token source.
// The project is used as the Kubernetes namespace for all scoped operations.
func NewClient(apiEndpoint, project string, tokenSource *auth.TokenSource) (*Client, error) {
	if tokenSource == nil {
		return nil, fmt.Errorf("token source is required")
	}
	if project == "" {
		return nil, fmt.Errorf("project is required (set --project, GCPHCPCTL_PROJECT, or project in config)")
	}
	if !strings.HasPrefix(apiEndpoint, "https://") {
		return nil, fmt.Errorf("API endpoint must use HTTPS: %s", apiEndpoint)
	}

	apiEndpoint = strings.TrimRight(apiEndpoint, "/")

	cfg := &rest.Config{
		Host:    apiEndpoint,
		APIPath: "/apis",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &gcpv1.GroupVersion,
			NegotiatedSerializer: codecs.WithoutConversion(),
		},
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &tokenTransport{base: rt, tokenSource: tokenSource}
		},
	}

	rc, err := rest.RESTClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating REST client: %w", err)
	}

	return &Client{restClient: rc, project: project}, nil
}

// tokenTransport injects an Authorization header using the auth.TokenSource.
type tokenTransport struct {
	base        http.RoundTripper
	tokenSource *auth.TokenSource
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, _, err := t.tokenSource.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("obtaining auth token: %w", err)
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(req)
}

// Project returns the GCP project ID (Kubernetes namespace) this client is scoped to.
func (c *Client) Project() string {
	return c.project
}

// Namespace returns the Kubernetes namespace for the configured project.
func (c *Client) Namespace() string {
	return NamespaceForProject(c.project)
}

// Clusters returns a ClusterInterface for performing cluster operations.
func (c *Client) Clusters() ClusterInterface {
	return &clusterClient{restClient: c.restClient}
}

// ClusterInterface defines operations on Cluster resources.
// Namespace is transparent to the caller — list is cluster-scoped,
// and mutating operations use the namespace from the resource metadata.
type ClusterInterface interface {
	Create(ctx context.Context, namespace string, cluster *gcpv1.Cluster) (*gcpv1.Cluster, error)
	Get(ctx context.Context, namespace, name string) (*gcpv1.Cluster, error)
	List(ctx context.Context) (*gcpv1.ClusterList, error)
	Delete(ctx context.Context, namespace, name string) error
}

type clusterClient struct {
	restClient rest.Interface
}

func (c *clusterClient) Create(ctx context.Context, namespace string, cluster *gcpv1.Cluster) (*gcpv1.Cluster, error) {
	result := &gcpv1.Cluster{}
	err := c.restClient.Post().
		Namespace(namespace).
		Resource("clusters").
		Body(cluster).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *clusterClient) Get(ctx context.Context, namespace, name string) (*gcpv1.Cluster, error) {
	result := &gcpv1.Cluster{}
	err := c.restClient.Get().
		Namespace(namespace).
		Resource("clusters").
		Name(name).
		Do(ctx).
		Into(result)
	return result, err
}

// List returns all clusters across all namespaces (cluster-scoped list).
func (c *clusterClient) List(ctx context.Context) (*gcpv1.ClusterList, error) {
	result := &gcpv1.ClusterList{}
	err := c.restClient.Get().
		Resource("clusters").
		Do(ctx).
		Into(result)
	return result, err
}

func (c *clusterClient) Delete(ctx context.Context, namespace, name string) error {
	return c.restClient.Delete().
		Namespace(namespace).
		Resource("clusters").
		Name(name).
		Do(ctx).
		Error()
}

// ResolveCluster finds a cluster by name within the client's project namespace.
func (c *Client) ResolveCluster(ctx context.Context, name string) (*gcpv1.Cluster, error) {
	cluster, err := c.Clusters().Get(ctx, c.Namespace(), name)
	if err != nil {
		return nil, fmt.Errorf("looking up cluster %q in project %q: %w", name, c.project, err)
	}
	return cluster, nil
}

// NamespaceForProject returns the namespace for a given GCP project ID.
// The platform API server uses a per-project multi-tenancy model where
// the namespace equals the project ID.
func NamespaceForProject(projectID string) string {
	return projectID
}
