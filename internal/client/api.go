package client

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// SecretInfo reports a write-only secret's presence.
type SecretInfo struct {
	Set    bool   `json:"set"`
	Source string `json:"source"`
}

// Object is a generic managed configuration object.
type Object struct {
	ID         string                `json:"id"`
	Kind       string                `json:"kind"`
	Name       string                `json:"name"`
	Spec       json.RawMessage       `json:"spec"`
	Revision   int64                 `json:"revision"`
	ManagedBy  string                `json:"managed_by"`
	DriftState string                `json:"drift_state"`
	Secrets    map[string]SecretInfo `json:"secrets,omitempty"`
}

// WriteObject is the create/replace body.
type WriteObject struct {
	Name    string             `json:"name"`
	Spec    json.RawMessage    `json:"spec"`
	Secrets map[string]*string `json:"secrets,omitempty"`
}

// GetObject reads an object by id or name.
func (c *Client) GetObject(ctx context.Context, plural, idOrName string) (*Object, error) {
	var o Object
	return &o, c.Do(ctx, http.MethodGet, "/"+plural+"/"+Escape(idOrName), nil, &o, 0)
}

// CreateObject creates an object.
func (c *Client) CreateObject(ctx context.Context, plural string, w WriteObject) (*Object, error) {
	var o Object
	return &o, c.Do(ctx, http.MethodPost, "/"+plural, w, &o, 0)
}

// ReplaceObject replaces an object at revision rev.
func (c *Client) ReplaceObject(ctx context.Context, plural, id string, rev int64, w WriteObject) (*Object, error) {
	var o Object
	return &o, c.Do(ctx, http.MethodPut, "/"+plural+"/"+Escape(id), w, &o, rev)
}

// DeleteObject deletes an object at revision rev (0 = unconditional).
func (c *Client) DeleteObject(ctx context.Context, plural, id string, rev int64) error {
	return c.Do(ctx, http.MethodDelete, "/"+plural+"/"+Escape(id), nil, nil, rev)
}

// PlanVersion is a routing plan version.
type PlanVersion struct {
	ID         string          `json:"id"`
	PlanID     string          `json:"plan_id"`
	Version    int64           `json:"version"`
	Status     string          `json:"status"`
	Document   json.RawMessage `json:"document"`
	Note       string          `json:"note"`
	RollbackOf string          `json:"rollback_of,omitempty"`
}

// CreateDraft creates a draft version.
func (c *Client) CreateDraft(ctx context.Context, planID string, doc json.RawMessage, note string) (*PlanVersion, error) {
	var v PlanVersion
	body := map[string]any{"document": doc, "note": note}
	return &v, c.Do(ctx, http.MethodPost, "/routing-plans/"+Escape(planID)+"/versions", body, &v, 0)
}

// GetVersion reads a version.
func (c *Client) GetVersion(ctx context.Context, planID, versionID string) (*PlanVersion, error) {
	var v PlanVersion
	return &v, c.Do(ctx, http.MethodGet, "/routing-plans/"+Escape(planID)+"/versions/"+Escape(versionID), nil, &v, 0)
}

// ListVersions lists versions of a plan.
func (c *Client) ListVersions(ctx context.Context, planID string) ([]PlanVersion, error) {
	var out struct {
		Items []PlanVersion `json:"items"`
	}
	return out.Items, c.Do(ctx, http.MethodGet, "/routing-plans/"+Escape(planID)+"/versions", nil, &out, 0)
}

// PublishVersion publishes a draft.
func (c *Client) PublishVersion(ctx context.Context, planID, versionID string) (*PlanVersion, error) {
	var v PlanVersion
	return &v, c.Do(ctx, http.MethodPost, "/routing-plans/"+Escape(planID)+"/versions/"+Escape(versionID)+"/publish", nil, &v, 0)
}

// Rollback publishes a copy of a previously published version.
func (c *Client) Rollback(ctx context.Context, planID, versionID string) (*PlanVersion, error) {
	var v PlanVersion
	return &v, c.Do(ctx, http.MethodPost, "/routing-plans/"+Escape(planID)+"/rollback", map[string]string{"version_id": versionID}, &v, 0)
}

// DiscardDraft deletes a draft version.
func (c *Client) DiscardDraft(ctx context.Context, planID, versionID string) error {
	return c.Do(ctx, http.MethodDelete, "/routing-plans/"+Escape(planID)+"/versions/"+Escape(versionID), nil, nil, 0)
}

// Credential is gateway credential metadata.
type Credential struct {
	ID        string            `json:"id"`
	Prefix    string            `json:"prefix"`
	BindingID string            `json:"binding_id"`
	Name      string            `json:"name"`
	Scopes    []string          `json:"scopes"`
	BoundRefs map[string]string `json:"bound_refs"`
	ManagedBy string            `json:"managed_by"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
	RevokedAt *time.Time        `json:"revoked_at,omitempty"`
}

// IssueCredential is the issue request.
type IssueCredential struct {
	Name       string            `json:"name"`
	Scopes     []string          `json:"scopes,omitempty"`
	TTLSeconds int64             `json:"ttl_seconds,omitempty"`
	BoundRefs  map[string]string `json:"bound_refs,omitempty"`
}

// IssuedCredential holds the one-time token.
type IssuedCredential struct {
	Credential *Credential       `json:"credential"`
	Token      string            `json:"token"`
	BaseURLs   map[string]string `json:"base_urls"`
}

// IssueGatewayCredential issues a credential for a client binding.
func (c *Client) IssueGatewayCredential(ctx context.Context, bindingID string, in IssueCredential) (*IssuedCredential, error) {
	var out IssuedCredential
	return &out, c.Do(ctx, http.MethodPost, "/client-bindings/"+Escape(bindingID)+"/credentials", in, &out, 0)
}

// ListGatewayCredentials lists credentials of a binding.
func (c *Client) ListGatewayCredentials(ctx context.Context, bindingID string) ([]Credential, error) {
	var out struct {
		Items []Credential `json:"items"`
	}
	return out.Items, c.Do(ctx, http.MethodGet, "/client-bindings/"+Escape(bindingID)+"/credentials", nil, &out, 0)
}

// RevokeGatewayCredential revokes a credential.
func (c *Client) RevokeGatewayCredential(ctx context.Context, bindingID, credentialID string) error {
	return c.Do(ctx, http.MethodDelete, "/client-bindings/"+Escape(bindingID)+"/credentials/"+Escape(credentialID), nil, nil, 0)
}

// ServicePrincipal is a non-human API principal.
type ServicePrincipal struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Disabled    bool     `json:"disabled"`
	ManagedBy   string   `json:"managed_by"`
	Revision    int64    `json:"revision"`
}

// ServicePrincipalBody is the create/replace body.
type ServicePrincipalBody struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions"`
	Disabled    bool     `json:"disabled,omitempty"`
}

// GetServicePrincipal reads a service principal.
func (c *Client) GetServicePrincipal(ctx context.Context, id string) (*ServicePrincipal, error) {
	var sp ServicePrincipal
	return &sp, c.Do(ctx, http.MethodGet, "/service-principals/"+Escape(id), nil, &sp, 0)
}

// CreateServicePrincipal creates one.
func (c *Client) CreateServicePrincipal(ctx context.Context, b ServicePrincipalBody) (*ServicePrincipal, error) {
	var sp ServicePrincipal
	return &sp, c.Do(ctx, http.MethodPost, "/service-principals", b, &sp, 0)
}

// ReplaceServicePrincipal replaces one.
func (c *Client) ReplaceServicePrincipal(ctx context.Context, id string, rev int64, b ServicePrincipalBody) (*ServicePrincipal, error) {
	var sp ServicePrincipal
	return &sp, c.Do(ctx, http.MethodPut, "/service-principals/"+Escape(id), b, &sp, rev)
}

// DeleteServicePrincipal deletes one with its tokens.
func (c *Client) DeleteServicePrincipal(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/service-principals/"+Escape(id), nil, nil, 0)
}

// APIToken is API token metadata.
type APIToken struct {
	ID        string     `json:"id"`
	Prefix    string     `json:"prefix"`
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// IssueAPIToken issues a token; the plaintext is returned once.
func (c *Client) IssueAPIToken(ctx context.Context, spID, name string, ttlSeconds int64) (*APIToken, string, error) {
	var out struct {
		Token     APIToken `json:"token"`
		Plaintext string   `json:"plaintext"`
	}
	err := c.Do(ctx, http.MethodPost, "/service-principals/"+Escape(spID)+"/tokens", map[string]any{"name": name, "ttl_seconds": ttlSeconds}, &out, 0)
	return &out.Token, out.Plaintext, err
}

// ListAPITokens lists tokens of a service principal.
func (c *Client) ListAPITokens(ctx context.Context, spID string) ([]APIToken, error) {
	var out struct {
		Items []APIToken `json:"items"`
	}
	return out.Items, c.Do(ctx, http.MethodGet, "/service-principals/"+Escape(spID)+"/tokens", nil, &out, 0)
}

// RevokeAPIToken revokes a token.
func (c *Client) RevokeAPIToken(ctx context.Context, spID, tokenID string) error {
	return c.Do(ctx, http.MethodDelete, "/service-principals/"+Escape(spID)+"/tokens/"+Escape(tokenID), nil, nil, 0)
}

// AuthSettings are the authentication settings.
type AuthSettings struct {
	PasswordLogin          bool  `json:"password_login"`
	IdleTimeoutSeconds     int64 `json:"idle_timeout_seconds"`
	AbsoluteTimeoutSeconds int64 `json:"absolute_timeout_seconds"`
	Revision               int64 `json:"revision,omitempty"`
}

// GetAuthSettings reads the settings.
func (c *Client) GetAuthSettings(ctx context.Context) (*AuthSettings, error) {
	var s AuthSettings
	return &s, c.Do(ctx, http.MethodGet, "/auth/settings", nil, &s, 0)
}

// PutAuthSettings replaces the settings.
func (c *Client) PutAuthSettings(ctx context.Context, rev int64, s AuthSettings) (*AuthSettings, error) {
	var out AuthSettings
	body := map[string]any{"password_login": s.PasswordLogin, "idle_timeout_seconds": s.IdleTimeoutSeconds, "absolute_timeout_seconds": s.AbsoluteTimeoutSeconds}
	return &out, c.Do(ctx, http.MethodPut, "/auth/settings", body, &out, rev)
}

// RoleMapping maps an identity-provider group to a role.
type RoleMapping struct {
	Group string `json:"group"`
	Role  string `json:"role"`
}

// OIDCProvider is an SSO provider.
type OIDCProvider struct {
	ID                  string        `json:"id"`
	Slug                string        `json:"slug"`
	DisplayName         string        `json:"display_name"`
	Issuer              string        `json:"issuer"`
	ClientID            string        `json:"client_id"`
	ClientSecretSet     bool          `json:"client_secret_set"`
	ClientSecretRef     string        `json:"client_secret_ref,omitempty"`
	Scopes              []string      `json:"scopes"`
	GroupsClaim         string        `json:"groups_claim"`
	RoleMappings        []RoleMapping `json:"role_mappings"`
	DefaultRole         string        `json:"default_role,omitempty"`
	Provisioning        string        `json:"provisioning"`
	AllowedDomains      []string      `json:"allowed_domains"`
	AllowedAlgs         []string      `json:"allowed_algs"`
	AllowPrivateNetwork bool          `json:"allow_private_network"`
	Enabled             bool          `json:"enabled"`
	RedirectURI         string        `json:"redirect_uri"`
	ManagedBy           string        `json:"managed_by"`
	Revision            int64         `json:"revision"`
}

// OIDCProviderBody is the create/replace body.
type OIDCProviderBody struct {
	Slug                string        `json:"slug"`
	DisplayName         string        `json:"display_name"`
	Issuer              string        `json:"issuer"`
	ClientID            string        `json:"client_id"`
	ClientSecret        *string       `json:"client_secret,omitempty"`
	ClientSecretRef     string        `json:"client_secret_ref,omitempty"`
	Scopes              []string      `json:"scopes,omitempty"`
	GroupsClaim         string        `json:"groups_claim,omitempty"`
	RoleMappings        []RoleMapping `json:"role_mappings,omitempty"`
	DefaultRole         string        `json:"default_role,omitempty"`
	Provisioning        string        `json:"provisioning"`
	AllowedDomains      []string      `json:"allowed_domains,omitempty"`
	AllowedAlgs         []string      `json:"allowed_algs,omitempty"`
	AllowPrivateNetwork bool          `json:"allow_private_network,omitempty"`
	Enabled             bool          `json:"enabled"`
}

// GetOIDCProvider reads one.
func (c *Client) GetOIDCProvider(ctx context.Context, id string) (*OIDCProvider, error) {
	var p OIDCProvider
	return &p, c.Do(ctx, http.MethodGet, "/auth/oidc-providers/"+Escape(id), nil, &p, 0)
}

// CreateOIDCProvider creates one.
func (c *Client) CreateOIDCProvider(ctx context.Context, b OIDCProviderBody) (*OIDCProvider, error) {
	var p OIDCProvider
	return &p, c.Do(ctx, http.MethodPost, "/auth/oidc-providers", b, &p, 0)
}

// ReplaceOIDCProvider replaces one.
func (c *Client) ReplaceOIDCProvider(ctx context.Context, id string, rev int64, b OIDCProviderBody) (*OIDCProvider, error) {
	var p OIDCProvider
	return &p, c.Do(ctx, http.MethodPut, "/auth/oidc-providers/"+Escape(id), b, &p, rev)
}

// DeleteOIDCProvider deletes one.
func (c *Client) DeleteOIDCProvider(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/auth/oidc-providers/"+Escape(id), nil, nil, 0)
}

// GetJSON reads any endpoint into out (data sources).
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out, 0)
}
