// Package client is a small HTTP client for the Harbor Pilot control-plane
// API (/api/v1). It never logs request or response bodies, and errors carry
// only the server's error envelope (code, message, request id and field
// locations), never submitted values.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ManagedBy is sent on every request so the server records Terraform as
// the change source and can flag out-of-band edits as drift.
const ManagedBy = "terraform"

// Options configure a Client.
type Options struct {
	// BaseURL is the server origin, e.g. https://pilot.example.com.
	BaseURL            string
	Token              string
	CACertFile         string
	InsecureSkipVerify bool
	// Timeout bounds each request (default 30s).
	Timeout time.Duration
	// UserAgent identifies the provider version.
	UserAgent string
	// ManagedBy overrides the change source header (tests simulate
	// out-of-band edits with "api"). Default: terraform.
	ManagedBy string
}

// Client calls the control-plane API.
type Client struct {
	base      *url.URL
	token     string
	hc        *http.Client
	ua        string
	managedBy string
}

// New validates options and builds a Client.
func New(o Options) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("url must be an absolute http(s) URL such as https://pilot.example.com")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url scheme must be http or https")
	}
	if o.Token == "" {
		return nil, errors.New("an API token is required")
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: o.InsecureSkipVerify} //nolint:gosec // explicit operator opt-in
	if o.CACertFile != "" {
		pem, err := os.ReadFile(o.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("read ca_cert_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("ca_cert_file contains no PEM certificates")
		}
		tlsCfg.RootCAs = pool
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg
	tr.Proxy = http.ProxyFromEnvironment
	ua := o.UserAgent
	if ua == "" {
		ua = "terraform-provider-harbor-pilot"
	}
	mb := o.ManagedBy
	if mb == "" {
		mb = ManagedBy
	}
	return &Client{base: u, token: o.Token, managedBy: mb, hc: &http.Client{Timeout: o.Timeout, Transport: tr,
		// Never follow redirects: the token must only reach the configured origin.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, ua: ua}, nil
}

// FieldError is one validation problem reported by the server.
type FieldError struct {
	Location string `json:"location"`
	Message  string `json:"message"`
}

// Error is a non-2xx API response.
type Error struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Fields    []FieldError
	// ReferencedBy lists objects that block a delete (409).
	ReferencedBy []string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (HTTP %d", e.Message, e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, ", %s", e.Code)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, ", request %s", e.RequestID)
	}
	b.WriteString(")")
	for _, f := range e.Fields {
		fmt.Fprintf(&b, "\n  %s: %s", f.Location, f.Message)
	}
	if len(e.ReferencedBy) > 0 {
		fmt.Fprintf(&b, "\n  referenced by: %s", strings.Join(e.ReferencedBy, ", "))
	}
	return b.String()
}

// IsNotFound reports a 404.
func IsNotFound(err error) bool { return status(err) == http.StatusNotFound }

// IsReferenced reports a delete refused because of references: 409, or
// 422 when the deletion would invalidate a published routing plan.
func IsReferenced(err error) bool {
	s := status(err)
	return s == http.StatusConflict || s == http.StatusUnprocessableEntity
}

// IsPreconditionFailed reports a 412 (stale If-Match).
func IsPreconditionFailed(err error) bool { return status(err) == http.StatusPreconditionFailed }

func status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// envelope mirrors {"error":{"code","message","request_id","details"}}.
// Field "value" entries in details are deliberately not decoded: they echo
// submitted input, which may contain secrets.
type envelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Details   struct {
			Fields       []FieldError `json:"fields"`
			ReferencedBy []string     `json:"referenced_by"`
		} `json:"details"`
	} `json:"error"`
}

// ETag formats an If-Match value for a revision.
func ETag(rev int64) string { return `"r` + strconv.FormatInt(rev, 10) + `"` }

// Do sends a request. in is JSON-encoded when non-nil; out is decoded when
// non-nil and the response has a body. ifMatch, when > 0, adds If-Match.
func (c *Client) Do(ctx context.Context, method, path string, in, out any, ifMatch int64) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	attempts := 1
	if method == http.MethodGet {
		attempts = 4
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			d := time.Duration(200*(1<<(i-1)))*time.Millisecond + time.Duration(rand.IntN(100))*time.Millisecond //nolint:gosec // retry jitter
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		retry, err := c.once(ctx, method, path, body, out, ifMatch)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry {
			return err
		}
	}
	return lastErr
}

func (c *Client) once(ctx context.Context, method, path string, body []byte, out any, ifMatch int64) (bool, error) {
	u := c.base.String() + "/api/v1" + path
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("X-HP-Managed-By", c.managedBy)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ifMatch > 0 {
		req.Header.Set("If-Match", ETag(ifMatch))
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		// Transport errors never include the token (it is a header).
		return method == http.MethodGet, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return method == http.MethodGet, err
	}
	if resp.StatusCode >= 300 {
		e := &Error{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		var env envelope
		if json.Unmarshal(data, &env) == nil && env.Error.Message != "" {
			e.Code, e.Message, e.RequestID = env.Error.Code, env.Error.Message, env.Error.RequestID
			e.Fields, e.ReferencedBy = env.Error.Details.Fields, env.Error.Details.ReferencedBy
		}
		retry := method == http.MethodGet && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable)
		return retry, e
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return false, fmt.Errorf("decode %s %s response: %w", method, path, err)
		}
	}
	return false, nil
}

// Escape escapes one path segment.
func Escape(s string) string { return url.PathEscape(s) }
