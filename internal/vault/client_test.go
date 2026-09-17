package vault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestKVv2Operations(t *testing.T) {
	t.Helper()
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Vault-Token"); got != "test-token" {
			t.Errorf("token = %q", got)
		}
		if got := r.Header.Get("X-Vault-Namespace"); got != "team" {
			t.Errorf("namespace = %q", got)
		}
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/secret/metadata/apps":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"keys": []string{"z", "folder/", "a"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/secret/data/apps/a":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]any{"password": "s3cret"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/secret/data/apps/a":
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body.Data, map[string]any{"password": "new"}) {
				t.Errorf("write body = %#v", body.Data)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/secret/metadata/apps/a":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "team", "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	keys, err := client.List(ctx, "apps")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"folder/", "a", "z"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %#v, want %#v", keys, want)
	}
	data, err := client.Read(ctx, "apps/a")
	if err != nil {
		t.Fatal(err)
	}
	if data["password"] != "s3cret" {
		t.Fatalf("data = %#v", data)
	}
	if err := client.Write(ctx, "apps/a", map[string]any{"password": "new"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "apps/a"); err != nil {
		t.Fatal(err)
	}
	if requests[0] != "GET /v1/secret/metadata/apps?list=true" {
		t.Fatalf("list request = %q", requests[0])
	}
}

func TestVaultErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{"errors": []string{"permission denied"}})
	}))
	defer server.Close()
	client, err := New(server.URL, "token", "", "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(context.Background(), "x"); err == nil || err.Error() != "Vault: permission denied" {
		t.Fatalf("error = %v", err)
	}
}

func TestNewRejectsInvalidAddress(t *testing.T) {
	for _, address := range []string{"", "vault.local", "ftp://vault.local"} {
		if _, err := New(address, "token", "", "secret", false); err == nil {
			t.Errorf("New(%q) succeeded", address)
		}
	}
}
