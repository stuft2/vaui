package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stuft2/vaui/internal/secret"
)

type Client struct {
	base      *url.URL
	token     string
	namespace string
	mount     string
	http      *http.Client
}

func New(address, token, namespace, mount string, insecure bool) (*Client, error) {
	base, err := url.Parse(address)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("invalid Vault address %q", address)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} //nolint:gosec -- explicit CLI option
	return &Client{
		base: base, token: token, namespace: namespace, mount: strings.Trim(mount, "/"),
		http: &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}, nil
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
	u.Path = path.Join(c.base.Path, "v1", c.mount, kind, strings.Trim(name, "/"))
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
		return fmt.Errorf("Vault request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		var payload struct {
			Errors []string `json:"errors"`
		}
		_ = json.Unmarshal(message, &payload)
		if len(payload.Errors) > 0 {
			return fmt.Errorf("Vault: %s", strings.Join(payload.Errors, "; "))
		}
		return fmt.Errorf("Vault: %s", res.Status)
	}
	if target != nil {
		if err := json.NewDecoder(res.Body).Decode(target); err != nil {
			return fmt.Errorf("decode Vault response: %w", err)
		}
	}
	return nil
}
