package auth

import (
	"errors"
	"fmt"
	"strings"
)

// identityFromClaims maps ID token claims to a Kubernetes identity. It adds the prefixes and
// rejects an unverified email as the user name.
func identityFromClaims(claims map[string]any, o *OIDCConfig) (*Identity, error) {
	user, _ := claims[o.UsernameClaim].(string)
	if user == "" {
		return nil, fmt.Errorf("the ID token has no %q claim", o.UsernameClaim)
	}
	if o.UsernameClaim == "email" {
		if verified, ok := claims["email_verified"].(bool); ok && !verified {
			return nil, errors.New("the email address is not verified")
		}
	}
	id := &Identity{User: o.UsernamePrefix + user}
	for _, g := range stringList(claims[o.GroupsClaim]) {
		id.Groups = append(id.Groups, o.GroupsPrefix+g)
	}
	if strings.HasPrefix(id.User, "system:") {
		return nil, errors.New(`a user name cannot start with "system:"`)
	}
	return id, nil
}

// stringList reads a claim that is a list of strings or one string.
func stringList(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
