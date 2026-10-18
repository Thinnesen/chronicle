// Package auth logs users in with OIDC and authenticates API requests.
// It maps claims to Kubernetes users and groups with a prefix, so that no user gets a system name.
package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Config is the login configuration of the server.
type Config struct {
	OIDC    OIDCConfig    `json:"oidc"`
	Session SessionConfig `json:"session"`
	// InsecureCookies permits cookies without the Secure flag, for local development over HTTP only.
	InsecureCookies bool `json:"insecureCookies,omitempty"`
}

// OIDCConfig configures one OIDC provider.
type OIDCConfig struct {
	Issuer           string   `json:"issuer"`
	ClientID         string   `json:"clientID"`
	ClientSecretFile string   `json:"clientSecretFile,omitempty"`
	RedirectURL      string   `json:"redirectURL"`
	Scopes           []string `json:"scopes,omitempty"`
	// UsernameClaim is the claim for the Kubernetes user name. The default is "email".
	UsernameClaim string `json:"usernameClaim,omitempty"`
	// GroupsClaim is the claim for the Kubernetes groups. The default is "groups".
	GroupsClaim string `json:"groupsClaim,omitempty"`
	// UsernamePrefix and GroupsPrefix are added to the names. The default is "oidc:". They cannot be empty.
	UsernamePrefix string `json:"usernamePrefix,omitempty"`
	GroupsPrefix   string `json:"groupsPrefix,omitempty"`
	// ExtraAudiences are other client IDs whose ID tokens the API accepts as bearer tokens.
	ExtraAudiences []string `json:"extraAudiences,omitempty"`
	// CLIClientID is the public client of the CLI for the device flow. Its ID tokens are accepted as bearer tokens.
	CLIClientID string `json:"cliClientID,omitempty"`
	// CAFile is a PEM file with the CA of the provider, when it is not in the system roots.
	CAFile string `json:"caFile,omitempty"`
	// Entra configures Microsoft Entra ID (Microsoft 365). Then the issuer comes from the tenant.
	Entra *EntraConfig `json:"entra,omitempty"`

	clientSecret string
}

// EntraConfig configures Microsoft Entra ID.
type EntraConfig struct {
	// TenantID is the only tenant that can log in.
	TenantID string `json:"tenantID"`
	// GroupsFrom is "groups" (security group object IDs, the default) or "roles" (app roles).
	GroupsFrom string `json:"groupsFrom,omitempty"`
	// Authority is the login host. The default is https://login.microsoftonline.com. Tests change it.
	Authority string `json:"authority,omitempty"`
	// GraphURL is the Microsoft Graph API. The default is https://graph.microsoft.com. Tests change it.
	GraphURL string `json:"graphURL,omitempty"`
}

// SessionConfig configures the server-side sessions.
type SessionConfig struct {
	// TTL is the longest life of a session. The default is 8h.
	TTL Duration `json:"ttl,omitempty"`
	// IdleTimeout ends a session that is not used. The default is 1h.
	IdleTimeout Duration `json:"idleTimeout,omitempty"`
}

// Duration is a time.Duration that reads "8h" from YAML.
type Duration struct{ time.Duration }

// UnmarshalJSON reads a duration such as "90m".
func (d *Duration) UnmarshalJSON(b []byte) error {
	v, err := time.ParseDuration(strings.Trim(string(b), `"`))
	d.Duration = v
	return err
}

// LoadConfig reads the configuration file and the client secret, and sets the defaults.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the login configuration: %w", err)
	}
	var c Config
	if err := yaml.UnmarshalStrict(data, &c); err != nil {
		return nil, fmt.Errorf("parse the login configuration: %w", err)
	}
	if err := c.complete(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) complete() error {
	o := &c.OIDC
	if e := o.Entra; e != nil {
		if err := completeEntra(o, e); err != nil {
			return err
		}
	}
	if o.Issuer == "" || o.ClientID == "" || o.RedirectURL == "" {
		return errors.New("oidc.issuer, oidc.clientID and oidc.redirectURL are required")
	}
	if o.ClientSecretFile != "" {
		b, err := os.ReadFile(o.ClientSecretFile)
		if err != nil {
			return fmt.Errorf("read the client secret: %w", err)
		}
		o.clientSecret = strings.TrimSpace(string(b))
	}
	if len(o.Scopes) == 0 {
		o.Scopes = []string{"openid", "profile", claimEmail, claimGroups}
	}
	if o.CLIClientID != "" {
		o.ExtraAudiences = append(o.ExtraAudiences, o.CLIClientID)
	}
	o.UsernameClaim = cmpOr(o.UsernameClaim, claimEmail)
	o.GroupsClaim = cmpOr(o.GroupsClaim, claimGroups)
	o.UsernamePrefix = cmpOr(o.UsernamePrefix, "oidc:")
	o.GroupsPrefix = cmpOr(o.GroupsPrefix, "oidc:")
	if strings.HasPrefix(o.UsernamePrefix, "system:") || strings.HasPrefix(o.GroupsPrefix, "system:") {
		return errors.New(`a prefix cannot start with "system:"`)
	}
	if c.Session.TTL.Duration == 0 {
		c.Session.TTL.Duration = 8 * time.Hour
	}
	if c.Session.IdleTimeout.Duration == 0 {
		c.Session.IdleTimeout.Duration = time.Hour
	}
	return nil
}

// completeEntra sets the Entra defaults: the tenant issuer, the oid user name and the entra prefixes.
func completeEntra(o *OIDCConfig, e *EntraConfig) error {
	if e.TenantID == "" || e.TenantID == "common" || e.TenantID == "organizations" {
		return errors.New("oidc.entra.tenantID must be the ID of one tenant")
	}
	e.Authority = strings.TrimSuffix(cmpOr(e.Authority, "https://login.microsoftonline.com"), "/")
	e.GraphURL = strings.TrimSuffix(cmpOr(e.GraphURL, "https://graph.microsoft.com"), "/")
	e.GroupsFrom = cmpOr(e.GroupsFrom, claimGroups)
	if e.GroupsFrom != claimGroups && e.GroupsFrom != "roles" {
		return errors.New(`oidc.entra.groupsFrom must be "groups" or "roles"`)
	}
	o.Issuer = e.Authority + "/" + e.TenantID + "/v2.0"
	o.UsernameClaim = cmpOr(o.UsernameClaim, "oid")
	o.GroupsClaim = cmpOr(o.GroupsClaim, e.GroupsFrom)
	o.UsernamePrefix = cmpOr(o.UsernamePrefix, "entra:")
	if e.GroupsFrom == "roles" {
		o.GroupsPrefix = cmpOr(o.GroupsPrefix, "entra:role:")
	}
	o.GroupsPrefix = cmpOr(o.GroupsPrefix, "entra:group:")
	if len(o.Scopes) == 0 {
		// User.Read gives an access token for Microsoft Graph, which the group overage needs.
		o.Scopes = []string{"openid", "profile", "email", "User.Read"}
	}
	return nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
