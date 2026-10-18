package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	groupCacheTTL = 10 * time.Minute
	maxGroupCache = 10000
)

// groupCache keeps the groups that Microsoft Graph returned, by user object ID.
type groupCache struct {
	mu      sync.Mutex
	entries map[string]cachedGroups
}

type cachedGroups struct {
	groups  []string
	expires time.Time
}

func (c *groupCache) get(oid string, now time.Time) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[oid]
	return e.groups, ok && now.Before(e.expires)
}

func (c *groupCache) put(oid string, groups []string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil || len(c.entries) >= maxGroupCache {
		c.entries = map[string]cachedGroups{}
	}
	c.entries[oid] = cachedGroups{groups: groups, expires: now.Add(groupCacheTTL)}
}

// checkEntra checks the tenant of the token and fills the groups from Microsoft Graph on a group overage.
// accessToken is empty for a bearer token; then only the cache can give the groups.
func (o *OIDC) checkEntra(ctx context.Context, claims map[string]any, accessToken string) error {
	e := o.cfg.OIDC.Entra
	if tid, _ := claims["tid"].(string); tid != e.TenantID {
		return fmt.Errorf("the token is from tenant %q, not from the configured tenant", tid)
	}
	if !hasOverage(claims, e.GroupsFrom) {
		return nil
	}
	oid, _ := claims["oid"].(string)
	if groups, ok := o.groups.get(oid, o.now()); ok {
		claims[e.GroupsFrom] = toAny(groups)
		return nil
	}
	if accessToken == "" {
		return errors.New("the user has too many groups for a token: log in to the UI first")
	}
	groups, err := o.graphGroups(ctx, accessToken)
	if err != nil {
		return err
	}
	o.groups.put(oid, groups, o.now())
	claims[e.GroupsFrom] = toAny(groups)
	return nil
}

// hasOverage tells if Entra left out the groups because the user has too many (more than 200 in a JWT).
func hasOverage(claims map[string]any, groupsClaim string) bool {
	if _, ok := claims[groupsClaim]; ok {
		return false
	}
	names, _ := claims["_claim_names"].(map[string]any)
	_, ok := names["groups"]
	return ok
}

// graphGroups reads the security groups of the user from Microsoft Graph.
func (o *OIDC) graphGroups(ctx context.Context, accessToken string) ([]string, error) {
	body := bytes.NewReader([]byte(`{"securityEnabledOnly":true}`))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.OIDC.Entra.GraphURL+"/v1.0/me/getMemberObjects", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read the groups from Microsoft Graph: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read the groups from Microsoft Graph: status %d", resp.StatusCode)
	}
	var out struct {
		Value []string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode the groups from Microsoft Graph: %w", err)
	}
	return out.Value, nil
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
