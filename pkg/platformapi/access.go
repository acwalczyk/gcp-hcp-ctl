package platformapi

import (
	"context"
	"fmt"
	"net/http"

	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"k8s.io/client-go/rest"
)

func (c *Client) Roles() RoleInterface { return &roleClient{restClient: c.restClient} }

// RoleInterface defines operations on Role resources.
type RoleInterface interface {
	Update(ctx context.Context, namespace, name string, role *gcpv1.Role) (*gcpv1.Role, error)
	Create(ctx context.Context, namespace string, role *gcpv1.Role) (*gcpv1.Role, error)
	Get(ctx context.Context, namespace, name string) (*gcpv1.Role, error)
	List(ctx context.Context, namespace string) (*gcpv1.RoleList, error)
	Delete(ctx context.Context, namespace, name string) error
}

type roleClient struct {
	restClient rest.Interface
}

func (c *roleClient) Create(ctx context.Context, namespace string, role *gcpv1.Role) (*gcpv1.Role, error) {
	if role == nil {
		return nil, fmt.Errorf("role must not be nil")
	}
	result := &gcpv1.Role{}
	response := c.restClient.Post().
		MaxRetries(0).
		Namespace(namespace).
		Resource("roles").
		Body(role).
		Do(ctx)
	if err := decodeResult(response, result, http.MethodPost, "roles", role.Name, true); err != nil {
		return nil, err
	}
	result.SetGroupVersionKind(gcpv1.GroupVersion.WithKind("Role"))
	return result, nil
}

func (c *roleClient) Get(ctx context.Context, namespace, name string) (*gcpv1.Role, error) {
	result := &gcpv1.Role{}
	response := c.restClient.Get().
		Namespace(namespace).
		Resource("roles").
		Name(name).
		Do(ctx)
	if err := decodeResult(response, result, http.MethodGet, "roles", name, false); err != nil {
		return nil, err
	}
	result.SetGroupVersionKind(gcpv1.GroupVersion.WithKind("Role"))
	return result, nil
}

// List returns roles in the given namespace.
func (c *roleClient) List(ctx context.Context, namespace string) (*gcpv1.RoleList, error) {
	var result *gcpv1.RoleList
	token := ""
	seen := map[string]bool{}
	for {
		page := &gcpv1.RoleList{}
		request := c.restClient.Get().Namespace(namespace).Resource("roles")
		if token != "" {
			request = request.Param("continue", token)
		}
		if err := decodeResult(request.Do(ctx), page, http.MethodGet, "roles", "", false); err != nil {
			return nil, err
		}
		if result == nil {
			result = page
		} else {
			result.Items = append(result.Items, page.Items...)
		}
		token = page.Continue
		if token == "" {
			break
		}
		if seen[token] {
			return nil, fmt.Errorf("listing roles: repeated pagination token")
		}
		seen[token] = true
	}
	result.Continue = ""
	result.RemainingItemCount = nil
	if result.Items == nil {
		result.Items = []gcpv1.Role{}
	}
	result.SetGroupVersionKind(gcpv1.GroupVersion.WithKind("RoleList"))
	return result, nil
}

func (c *roleClient) Delete(ctx context.Context, namespace, name string) error {
	response := c.restClient.Delete().
		MaxRetries(0).
		Namespace(namespace).
		Resource("roles").
		Name(name).
		Do(ctx)
	return normalizeResult(response, http.MethodDelete, "roles", name, false)
}

func (c *roleClient) Update(ctx context.Context, namespace, name string, role *gcpv1.Role) (*gcpv1.Role, error) {
	result := &gcpv1.Role{}
	response := c.restClient.Put().MaxRetries(0).Namespace(namespace).Resource("roles").Name(name).Body(role).Do(ctx)
	if err := decodeResult(response, result, http.MethodPut, "roles", name, true); err != nil {
		return nil, err
	}
	result.SetGroupVersionKind(gcpv1.GroupVersion.WithKind("Role"))
	return result, nil
}

func (c *Client) RoleBindings() RoleBindingInterface {
	return &roleBindingClient{restClient: c.restClient}
}

// RoleBindingInterface defines operations on RoleBinding resources.
type RoleBindingInterface interface {
	Create(ctx context.Context, namespace string, roleBinding *gcpv1.RoleBinding) (*gcpv1.RoleBinding, error)
	Get(ctx context.Context, namespace, name string) (*gcpv1.RoleBinding, error)
	List(ctx context.Context, namespace string) (*gcpv1.RoleBindingList, error)
	Delete(ctx context.Context, namespace, name string) error
}

type roleBindingClient struct {
	restClient rest.Interface
}

func (c *roleBindingClient) Create(ctx context.Context, namespace string, roleBinding *gcpv1.RoleBinding) (*gcpv1.RoleBinding, error) {
	if roleBinding == nil {
		return nil, fmt.Errorf("role binding must not be nil")
	}
	result := &gcpv1.RoleBinding{}
	response := c.restClient.Post().
		MaxRetries(0).
		Namespace(namespace).
		Resource("rolebindings").
		Body(roleBinding).
		Do(ctx)
	if err := decodeResult(response, result, http.MethodPost, "rolebindings", roleBinding.Name, true); err != nil {
		return nil, err
	}
	result.SetGroupVersionKind(gcpv1.GroupVersion.WithKind("RoleBinding"))
	return result, nil
}

func (c *roleBindingClient) Get(ctx context.Context, namespace, name string) (*gcpv1.RoleBinding, error) {
	result := &gcpv1.RoleBinding{}
	response := c.restClient.Get().
		Namespace(namespace).
		Resource("rolebindings").
		Name(name).
		Do(ctx)
	if err := decodeResult(response, result, http.MethodGet, "rolebindings", name, false); err != nil {
		return nil, err
	}
	result.SetGroupVersionKind(gcpv1.GroupVersion.WithKind("RoleBinding"))
	return result, nil
}

// List returns roleBindings in the given namespace.
func (c *roleBindingClient) List(ctx context.Context, namespace string) (*gcpv1.RoleBindingList, error) {
	var result *gcpv1.RoleBindingList
	token := ""
	seen := map[string]bool{}
	for {
		page := &gcpv1.RoleBindingList{}
		request := c.restClient.Get().Namespace(namespace).Resource("rolebindings")
		if token != "" {
			request = request.Param("continue", token)
		}
		if err := decodeResult(request.Do(ctx), page, http.MethodGet, "rolebindings", "", false); err != nil {
			return nil, err
		}
		if result == nil {
			result = page
		} else {
			result.Items = append(result.Items, page.Items...)
		}
		token = page.Continue
		if token == "" {
			break
		}
		if seen[token] {
			return nil, fmt.Errorf("listing rolebindings: repeated pagination token")
		}
		seen[token] = true
	}
	result.Continue = ""
	result.RemainingItemCount = nil
	if result.Items == nil {
		result.Items = []gcpv1.RoleBinding{}
	}
	result.SetGroupVersionKind(gcpv1.GroupVersion.WithKind("RoleBindingList"))
	return result, nil
}

func (c *roleBindingClient) Delete(ctx context.Context, namespace, name string) error {
	response := c.restClient.Delete().
		MaxRetries(0).
		Namespace(namespace).
		Resource("rolebindings").
		Name(name).
		Do(ctx)
	return normalizeResult(response, http.MethodDelete, "rolebindings", name, false)
}
