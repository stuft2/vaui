package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
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
			password := "s3cret"
			version := 3
			if r.URL.Query().Get("version") == "2" {
				password, version = "old", 2
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"data":     map[string]any{"password": password},
				"metadata": map[string]any{"version": version, "created_time": "2026-09-18T12:00:00Z", "deletion_time": "", "destroyed": false},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/secret/metadata/apps/a":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"current_version": 3,
				"versions": map[string]any{
					"3": map[string]any{"created_time": "2026-09-18T12:00:00Z", "deletion_time": "", "destroyed": false},
					"2": map[string]any{"created_time": "2026-09-17T12:00:00Z", "deletion_time": "2026-09-18T11:00:00Z", "destroyed": false},
					"1": map[string]any{"created_time": "2026-09-16T12:00:00Z", "deletion_time": "", "destroyed": true},
				},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/secret/data/apps/a":
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body.Data, map[string]any{"password": "new"}) {
				t.Errorf("write body = %#v", body.Data)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/secret/data/apps/a":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && (r.URL.Path == "/v1/secret/undelete/apps/a" || r.URL.Path == "/v1/secret/destroy/apps/a"):
			var body struct {
				Versions []int `json:"versions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body.Versions, []int{2}) {
				t.Errorf("versions body = %#v", body.Versions)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "team", []string{"secret"}, false)
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
	value, err := client.Read(ctx, "apps/a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if value.Data["password"] != "s3cret" || value.Version != 3 || value.CreatedTime.IsZero() {
		t.Fatalf("value = %#v", value)
	}
	metadata, err := client.Metadata(ctx, "apps/a")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CurrentVersion != 3 || len(metadata.Versions) != 3 || metadata.Versions[0].Version != 3 || metadata.Versions[1].DeletionTime == nil || !metadata.Versions[2].Destroyed {
		t.Fatalf("metadata = %#v", metadata)
	}
	old, err := client.Read(ctx, "apps/a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if old.Data["password"] != "old" || old.Version != 2 {
		t.Fatalf("old value = %#v", old)
	}
	if err := client.Write(ctx, "apps/a", map[string]any{"password": "new"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "apps/a"); err != nil {
		t.Fatal(err)
	}
	if err := client.Undelete(ctx, "apps/a", 2); err != nil {
		t.Fatal(err)
	}
	if err := client.Destroy(ctx, "apps/a", 2); err != nil {
		t.Fatal(err)
	}
	if requests[0] != "GET /v1/secret/metadata/apps?list=true" {
		t.Fatalf("list request = %q", requests[0])
	}
	wantTail := []string{"DELETE /v1/secret/data/apps/a", "POST /v1/secret/undelete/apps/a", "POST /v1/secret/destroy/apps/a"}
	if !reflect.DeepEqual(requests[len(requests)-3:], wantTail) {
		t.Fatalf("destructive requests = %#v, want %#v", requests[len(requests)-3:], wantTail)
	}
}

func TestRestoreWritesPriorVersionAsNewData(t *testing.T) {
	var written map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("version") != "2" {
				t.Errorf("version query = %q", r.URL.Query().Get("version"))
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]any{"password": "old"}, "metadata": map[string]any{"version": 2, "created_time": time.Now().UTC()}}})
		case http.MethodPost:
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			written = body.Data
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "token", "", []string{"secret"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(context.Background(), "apps/a", 2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(written, map[string]any{"password": "old"}) {
		t.Fatalf("written = %#v", written)
	}
}

func TestVaultErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{"errors": []string{"permission denied"}})
	}))
	defer server.Close()
	client, err := New(server.URL, "token", "", []string{"secret"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(context.Background(), "x", 0); err == nil || !strings.Contains(err.Error(), "Vault permission denied") {
		t.Fatalf("error = %v", err)
	}
}

func TestActionableVaultErrors(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{{http.StatusForbidden, "permission denied"}, {http.StatusNotFound, "path not found"}}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				json.NewEncoder(w).Encode(map[string]any{"errors": []string{"raw detail"}})
			}))
			defer server.Close()
			client, _ := New(server.URL, "sensitive-token", "", []string{"secret"}, false)
			_, err := client.Read(context.Background(), "x", 0)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "raw detail") || strings.Contains(err.Error(), "sensitive-token") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestVaultAuthenticationErrorsAreSimple(t *testing.T) {
	tests := []struct {
		name   string
		status int
		errors []string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, errors: []string{"raw detail"}},
		{name: "invalid token", status: http.StatusForbidden, errors: []string{"invalid token", "permission denied"}},
		{name: "missing token", status: http.StatusForbidden, errors: []string{"missing client token"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				json.NewEncoder(w).Encode(map[string]any{"errors": tt.errors})
			}))
			defer server.Close()
			client, _ := New(server.URL, "sensitive-token", "", []string{"secret"}, false)
			_, err := client.Read(context.Background(), "x", 0)
			if !errors.Is(err, ErrNotLoggedIn) || err.Error() != "Not logged in to Vault." {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestConnectivityErrorAndSafeContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	credentialAddress := strings.Replace(address, "http://", "http://user:password@", 1)
	client, err := New(credentialAddress, "token", "team", []string{"secret"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "connectivity failed") || strings.Contains(err.Error(), "password") {
		t.Fatalf("error = %v", err)
	}
	withUserInfo, err := New("https://user:password@vault.example.edu/base?credential=x", "token", "team", []string{"secret"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := withUserInfo.Address(); got != "https://vault.example.edu/base" {
		t.Fatalf("safe address = %q", got)
	}
}

func TestNewRejectsInvalidAddress(t *testing.T) {
	for _, address := range []string{"", "vault.local", "ftp://vault.local"} {
		if _, err := New(address, "token", "", []string{"secret"}, false); err == nil {
			t.Errorf("New(%q) succeeded", address)
		}
	}
}

func TestNewDiscoversAccessibleKVv2Mounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/sys/internal/ui/mounts" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Vault-Token"); got != "test-token" {
			t.Errorf("token = %q", got)
		}
		if got := r.Header.Get("X-Vault-Namespace"); got != "team" {
			t.Errorf("namespace = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"secret": map[string]any{
			"shared/": map[string]any{"type": "kv", "options": map[string]string{"version": "2"}},
			"legacy/": map[string]any{"type": "kv", "options": map[string]string{"version": "1"}},
			"apps/":   map[string]any{"type": "kv", "options": map[string]string{"version": "2"}},
			"pki/":    map[string]any{"type": "pki"},
		}}})
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "team", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"apps", "shared"}; !reflect.DeepEqual(client.Mounts(), want) {
		t.Fatalf("mounts = %q, want %q", client.Mounts(), want)
	}
	if client.Mount() != "apps" {
		t.Fatalf("selected mount = %q, want apps", client.Mount())
	}
}

func TestNewUsesConfiguredMountsWithoutDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unexpected discovery request")
	}))
	defer server.Close()

	client, err := New(server.URL, "token", "", []string{"configured"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if client.Mount() != "configured" {
		t.Fatalf("selected mount = %q", client.Mount())
	}
}

func TestNewReturnsActionableDiscoveryErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   any
		want   string
	}{
		{name: "request rejected", status: http.StatusForbidden, want: "detect KV v2 mounts"},
		{name: "none visible", status: http.StatusOK, body: map[string]any{"data": map[string]any{"secret": map[string]any{}}}, want: "no accessible KV v2 mounts detected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				if tt.body != nil {
					json.NewEncoder(w).Encode(tt.body)
				}
			}))
			defer server.Close()

			_, err := New(server.URL, "token", "", nil, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "-mount") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNewReportsInvalidTokenWithoutDiscoveryHints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{"errors": []string{"invalid token", "permission denied"}})
	}))
	defer server.Close()

	_, err := New(server.URL, "expired-token", "", nil, false)
	if !errors.Is(err, ErrNotLoggedIn) || err.Error() != "Not logged in to Vault." {
		t.Fatalf("error = %v", err)
	}
}
