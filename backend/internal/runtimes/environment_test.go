package runtimes

import (
	"context"
	"errors"
	"testing"
)

type environmentTestStore map[string][]byte

func (s environmentTestStore) Put(context.Context, string, string, []byte) error { return nil }
func (s environmentTestStore) Delete(context.Context, string, string) error { return nil }
func (s environmentTestStore) Get(_ context.Context, scope, name string) ([]byte, error) {
	value, ok := s[scope+"/"+name]
	if !ok {
		return nil, errors.New("missing")
	}
	return append([]byte(nil), value...), nil
}

type environmentTestResolver struct {
	result ResolvedProject
}

func (r environmentTestResolver) Resolve(context.Context, string) (ResolvedProject, error) {
	return r.result, nil
}

func TestEnvironmentResolverSeparatesSecretStoreValues(t *testing.T) {
	resolver := NewEnvironmentResolver(environmentTestResolver{result: ResolvedProject{Config: RuntimeConfig{
		Environment: map[string]string{"APP_ENV": "dev", "DB_HOST": "user-host"},
		SecretEnvironment: map[string]SecretReference{
			"API_TOKEN": {Name: "token"},
		},
	}}}, environmentTestStore{"project:p1:runtime/token": []byte("secret-token")})
	got, err := resolver.ResolveEnvironment(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Plain["APP_ENV"] != "dev" || got.Plain["DB_HOST"] != "user-host" {
		t.Fatalf("plain environment missing: %#v", got.Plain)
	}
	if got.Sensitive["API_TOKEN"] != "secret-token" {
		t.Fatalf("SecretStore environment missing: %#v", got.Sensitive)
	}
	if _, ok := got.Plain["API_TOKEN"]; ok {
		t.Fatal("secret environment must not remain in plain map")
	}
}
