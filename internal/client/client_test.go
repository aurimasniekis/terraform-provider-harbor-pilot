package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func newTest(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{BaseURL: srv.URL, Token: "hpa_test_token"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestErrorEnvelopeNeverKeepsValues(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_failed","message":"validation failed","request_id":"r1",
			"details":{"fields":[{"location":"body.secrets.credential","message":"too long","value":"sk-super-secret"}]}}}`))
	})
	_, err := c.CreateObject(context.Background(), "provider-accounts", WriteObject{Name: "a", Spec: []byte(`{}`)})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("got %T", err)
	}
	if e.Status != 422 || e.Code != "validation_failed" || e.RequestID != "r1" || len(e.Fields) != 1 || e.Fields[0].Location != "body.secrets.credential" {
		t.Fatalf("decoded %+v", e)
	}
	if strings.Contains(err.Error(), "sk-super-secret") {
		t.Fatal("error message contains a submitted value")
	}
}

func TestHeaders(t *testing.T) {
	var got http.Header
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{"id":"wsp_1","revision":4}`))
	})
	if _, err := c.ReplaceObject(context.Background(), "workspaces", "wsp_1", 3, WriteObject{Name: "w", Spec: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"Authorization": "Bearer hpa_test_token", "If-Match": `"r3"`, "X-Hp-Managed-By": "terraform",
		"Content-Type": "application/json"} {
		if got.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, got.Get(k), want)
		}
	}
}

func TestRetries(t *testing.T) {
	tests := []struct {
		name   string
		method string
		status int
		want   int32
	}{
		{"GET retried on 503", http.MethodGet, http.StatusServiceUnavailable, 4},
		{"GET retried on 429", http.MethodGet, http.StatusTooManyRequests, 4},
		{"GET not retried on 404", http.MethodGet, http.StatusNotFound, 1},
		{"POST never retried", http.MethodPost, http.StatusServiceUnavailable, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			c := newTest(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
			})
			_ = c.Do(context.Background(), tt.method, "/x", nil, nil, 0)
			if calls.Load() != tt.want {
				t.Fatalf("calls = %d, want %d", calls.Load(), tt.want)
			}
		})
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var followed atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Store(true) }))
	t.Cleanup(other.Close)
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	})
	err := c.Do(context.Background(), http.MethodGet, "/workspaces", nil, nil, 0)
	if err == nil || followed.Load() {
		t.Fatalf("redirect followed (err %v)", err)
	}
}

func TestNewValidates(t *testing.T) {
	tests := []struct {
		name string
		o    Options
	}{
		{"relative url", Options{BaseURL: "pilot.example.com", Token: "t"}},
		{"bad scheme", Options{BaseURL: "ftp://pilot.example.com", Token: "t"}},
		{"no token", Options{BaseURL: "https://pilot.example.com"}},
		{"missing ca file", Options{BaseURL: "https://pilot.example.com", Token: "t", CACertFile: "/nonexistent/ca.pem"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.o); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestStatusHelpers(t *testing.T) {
	if !IsNotFound(&Error{Status: 404}) || !IsPreconditionFailed(&Error{Status: 412}) || !IsReferenced(&Error{Status: 409}) || IsReferenced(&Error{Status: 400}) {
		t.Fatal("status helpers")
	}
}
