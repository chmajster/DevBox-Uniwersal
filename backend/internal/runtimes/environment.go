package runtimes

import (
	"context"
	"fmt"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

type ResolvedEnvironment struct {
	Plain     map[string]string
	Sensitive map[string]string
}

type EnvironmentResolver interface {
	ResolveEnvironment(ctx context.Context, projectID string) (ResolvedEnvironment, error)
}

type SecretEnvironmentResolver struct {
	resolver ProjectResolver
	secrets  secrets.SecretStore
}

func NewEnvironmentResolver(resolver ProjectResolver, secretStore secrets.SecretStore) *SecretEnvironmentResolver {
	return &SecretEnvironmentResolver{resolver: resolver, secrets: secretStore}
}

func (r *SecretEnvironmentResolver) ResolveEnvironment(ctx context.Context, projectID string) (ResolvedEnvironment, error) {
	if r == nil || r.resolver == nil {
		return ResolvedEnvironment{Plain: map[string]string{}, Sensitive: map[string]string{}}, nil
	}
	resolved, err := r.resolver.Resolve(ctx, projectID)
	if err != nil {
		return ResolvedEnvironment{}, err
	}
	result := ResolvedEnvironment{
		Plain:     copyStringMap(resolved.Config.Environment),
		Sensitive: make(map[string]string, len(resolved.Config.SecretEnvironment)),
	}
	for key, reference := range resolved.Config.SecretEnvironment {
		scope := strings.TrimSpace(reference.Scope)
		if scope == "" {
			scope = "project:" + projectID + ":runtime"
		}
		if r.secrets == nil {
			return ResolvedEnvironment{}, fmt.Errorf("runtime secret environment %s cannot be resolved because SecretStore is not configured", key)
		}
		plaintext, err := r.secrets.Get(ctx, scope, reference.Name)
		if err != nil {
			return ResolvedEnvironment{}, fmt.Errorf("runtime secret environment %s cannot be resolved", key)
		}
		result.Sensitive[key] = string(plaintext)
		clear(plaintext)
		delete(result.Plain, key)
	}
	return result, nil
}
