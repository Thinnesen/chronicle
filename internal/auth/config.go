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
	// ExtraAudiences are other client IDs whose ID tokens the API accepts as bearer tokens, for example the CLI.
	ExtraAudiences []string `json:"extraAudiences,omitempty"`
	// CAFile is a PEM file with the CA of the provider, when it is not in the system roots.
	CAFile string `json:"caFile,omitempty"`

	clientSecret string
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
		o.Scopes = []string{"openid", "profile", "email", "groups"}
	}
	o.UsernameClaim = cmpOr(o.UsernameClaim, "email")
	o.GroupsClaim = cmpOr(o.GroupsClaim, "groups")
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

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
