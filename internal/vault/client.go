package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stuft2/vaui/internal/secret"
)

type Client struct {
	base      *url.URL
	token     string
	namespace string
	mounts    []string
	mount     string
	mu        sync.RWMutex
	http      *http.Client
}

var ErrNotLoggedIn = errors.New("Not logged in to Vault.")

func New(address, token, namespace string, mounts []string, insecure bool) (*Client, error) {
	base, err := url.Parse(address)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("invalid Vault address %q", address)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} //nolint:gosec -- explicit CLI option
	client := &Client{
		base: base, token: token, namespace: namespace,
		http: &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}
	if len(mounts) == 0 {
		mounts, err = client.discoverKVv2Mounts(context.Background())
		if err != nil {
			if errors.Is(err, ErrNotLoggedIn) {
				return nil, err
			}
			return nil, fmt.Errorf("detect KV v2 mounts: %w; configure -mount or VAULT_KV2_MOUNTS", err)
		}
		if len(mounts) == 0 {
			return nil, fmt.Errorf("no accessible KV v2 mounts detected; configure -mount or VAULT_KV2_MOUNTS")
		}
	}
	client.mounts = append([]string(nil), mounts...)
	client.mount = mounts[0]
	return client, nil
}

func (c *Client) discoverKVv2Mounts(ctx context.Context) ([]string, error) {
	u := *c.base
	u.Path = path.Join(c.base.Path, "v1", "sys", "internal", "ui", "mounts")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", c.token)
	if c.namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.namespace)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, responseError(res)
	}
	var response struct {
		Data struct {
			Secret map[string]struct {
				Type    string            `json:"type"`
				Options map[string]string `json:"options"`
			} `json:"secret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Vault mount response: %w", err)
	}
	var mounts []string
	for mount, details := range response.Data.Secret {
		if details.Type == "kv" && details.Options["version"] == "2" {
			mounts = append(mounts, strings.Trim(mount, "/"))
		}
	}
	sort.Strings(mounts)
	return mounts, nil
}

func (c *Client) Mounts() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.mounts...)
}
func (c *Client) Mount() string { c.mu.RLock(); defer c.mu.RUnlock(); return c.mount }
func (c *Client) Address() string {
	value := *c.base
	value.User = nil
	value.RawQuery = ""
	value.Fragment = ""
	return strings.TrimSuffix(value.String(), "/")
}
func (c *Client) Namespace() string { return c.namespace }
func (c *Client) SelectMount(mount string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, candidate := range c.mounts {
		if candidate == mount {
			c.mount = mount
			return nil
		}
	}
	return fmt.Errorf("unknown KV v2 mount %q", mount)
}

func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	var response struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "metadata", prefix, map[string]string{"list": "true"}, nil, &response); err != nil {
		return nil, err
	}
	sort.Slice(response.Data.Keys, func(i, j int) bool {
		a, b := strings.HasSuffix(response.Data.Keys[i], "/"), strings.HasSuffix(response.Data.Keys[j], "/")
		if a != b {
			return a
		}
		return response.Data.Keys[i] < response.Data.Keys[j]
	})
	return response.Data.Keys, nil
}

func (c *Client) Read(ctx context.Context, name string, version int) (secret.Value, error) {
	var response struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata versionJSON    `json:"metadata"`
		} `json:"data"`
	}
	var query map[string]string
	if version > 0 {
		query = map[string]string{"version": strconv.Itoa(version)}
	}
	if err := c.request(ctx, http.MethodGet, "data", name, query, nil, &response); err != nil {
		return secret.Value{}, err
	}
	return response.Data.Metadata.value(response.Data.Data), nil
}

func (c *Client) Metadata(ctx context.Context, name string) (secret.Metadata, error) {
	var response struct {
		Data struct {
			CurrentVersion int                    `json:"current_version"`
			Versions       map[string]versionJSON `json:"versions"`
		} `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "metadata", name, nil, nil, &response); err != nil {
		return secret.Metadata{}, err
	}
	metadata := secret.Metadata{CurrentVersion: response.Data.CurrentVersion}
	for rawVersion, raw := range response.Data.Versions {
		version, err := strconv.Atoi(rawVersion)
		if err != nil {
			return secret.Metadata{}, fmt.Errorf("decode Vault response: invalid version %q", rawVersion)
		}
		metadata.Versions = append(metadata.Versions, raw.version(version))
	}
	sort.Slice(metadata.Versions, func(i, j int) bool { return metadata.Versions[i].Version > metadata.Versions[j].Version })
	return metadata, nil
}

func (c *Client) Restore(ctx context.Context, name string, version int) error {
	value, err := c.Read(ctx, name, version)
	if err != nil {
		return err
	}
	return c.Write(ctx, name, value.Data)
}

func (c *Client) Write(ctx context.Context, name string, data map[string]any) error {
	return c.request(ctx, http.MethodPost, "data", name, nil, map[string]any{"data": data}, nil)
}

type versionJSON struct {
	CreatedTime  time.Time `json:"created_time"`
	DeletionTime string    `json:"deletion_time"`
	Destroyed    bool      `json:"destroyed"`
	Version      int       `json:"version"`
}

func (v versionJSON) value(data map[string]any) secret.Value {
	return secret.Value{Data: data, Version: v.Version, CreatedTime: v.CreatedTime, DeletionTime: optionalTime(v.DeletionTime), Destroyed: v.Destroyed}
}

func (v versionJSON) version(number int) secret.Version {
	return secret.Version{Version: number, CreatedTime: v.CreatedTime, DeletionTime: optionalTime(v.DeletionTime), Destroyed: v.Destroyed}
}

func optionalTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func (c *Client) Delete(ctx context.Context, name string) error {
	return c.request(ctx, http.MethodDelete, "data", name, nil, nil, nil)
}

func (c *Client) Undelete(ctx context.Context, name string, version int) error {
	return c.request(ctx, http.MethodPost, "undelete", name, nil, map[string]any{"versions": []int{version}}, nil)
}

func (c *Client) Destroy(ctx context.Context, name string, version int) error {
	return c.request(ctx, http.MethodPost, "destroy", name, nil, map[string]any{"versions": []int{version}}, nil)
}

func (c *Client) request(ctx context.Context, method, kind, name string, query map[string]string, body any, target any) error {
	u := *c.base
	c.mu.RLock()
	mount := c.mount
	c.mu.RUnlock()
	u.Path = path.Join(c.base.Path, "v1", mount, kind, strings.Trim(name, "/"))
	q := u.Query()
	for key, value := range query {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", c.token)
	if c.namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		detail := err
		var urlError *url.Error
		if errors.As(err, &urlError) {
			detail = urlError.Err
		}
		return fmt.Errorf("Vault connectivity failed: check VAULT_ADDR and network access (%v)", detail)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return responseError(res)
	}
	if target != nil {
		if err := json.NewDecoder(res.Body).Decode(target); err != nil {
			return fmt.Errorf("decode Vault response: %w", err)
		}
	}
	return nil
}

func responseError(res *http.Response) error {
	message, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	var payload struct {
		Errors []string `json:"errors"`
	}
	_ = json.Unmarshal(message, &payload)
	details := res.Status
	if len(payload.Errors) > 0 {
		details = strings.Join(payload.Errors, "; ")
	}
	if res.StatusCode == http.StatusUnauthorized || containsError(payload.Errors, "invalid token") || containsError(payload.Errors, "missing client token") {
		return ErrNotLoggedIn
	}
	switch res.StatusCode {
	case http.StatusForbidden:
		return fmt.Errorf("Vault permission denied: request access for this mount and path (%s)", details)
	case http.StatusNotFound:
		return fmt.Errorf("Vault path not found: verify the mount and secret path (%s)", details)
	default:
		return fmt.Errorf("Vault request failed: %s", details)
	}
}

func containsError(messages []string, target string) bool {
	for _, message := range messages {
		if strings.Contains(strings.ToLower(message), target) {
			return true
		}
	}
	return false
}
