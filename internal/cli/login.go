package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

func (a *app) loginCommand() *cobra.Command {
	var tokenFile string
	cmd := &cobra.Command{
		Use:   "login --server <URL>",
		Short: "Log in to a chronicle server",
		Long: "Log in with the OIDC device flow, or with a Kubernetes service account token.\n" +
			"Create such a token with: kubectl create token <service-account> --audience=chronicle",
		Example: "  chronicle login --server https://chronicle.example.com -n team-a\n" +
			"  kubectl create token ci -n team-a --audience=chronicle | chronicle login --server https://chronicle.example.com --token-file -",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.server == "" {
				return errors.New("give the server with --server")
			}
			cfg := &Config{Server: strings.TrimSuffix(a.server, "/"), Namespace: a.namespace}
			if tokenFile != "" {
				tok, err := readToken(tokenFile, cmd.InOrStdin())
				if err != nil {
					return err
				}
				cfg.Token = tok
			} else if err := deviceLogin(cmd.Context(), cfg, a.out); err != nil {
				return err
			}
			if err := cfg.save(a.configPath); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(a.out, "Logged in to %s.\n", cfg.Server)
			return nil
		},
	}
	cmd.Flags().StringVar(&tokenFile, "token-file", "", `A file with a service account token, or "-" for the standard input.`)
	return cmd
}

func readToken(path string, stdin io.Reader) (string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(io.LimitReader(stdin, 64<<10))
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read the token: %w", err)
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", errors.New("the token is empty")
	}
	return tok, nil
}

// cliConfig is the answer of /auth/cli.
type cliConfig struct {
	Issuer   string   `json:"issuer"`
	ClientID string   `json:"clientID"`
	Scopes   []string `json:"scopes"`
}

// deviceLogin runs the OIDC device flow: the user confirms a code in a browser.
func deviceLogin(ctx context.Context, cfg *Config, out io.Writer) error {
	resp, err := http.Get(cfg.Server + "/auth/cli")
	if err != nil {
		return fmt.Errorf("ask the server for the login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("the server has no CLI login: use --token-file with a service account token")
	}
	var cc cliConfig
	if err := json.NewDecoder(resp.Body).Decode(&cc); err != nil {
		return fmt.Errorf("read the login of the server: %w", err)
	}
	oc, err := oauthConfig(ctx, cc.Issuer, cc.ClientID, cc.Scopes)
	if err != nil {
		return err
	}
	da, err := oc.DeviceAuth(ctx)
	if err != nil {
		return fmt.Errorf("start the device login: %w", err)
	}
	_, _ = fmt.Fprintf(out, "Open %s and type the code %s\n", da.VerificationURI, da.UserCode)
	if da.VerificationURIComplete != "" {
		_, _ = fmt.Fprintf(out, "Or open %s\n", da.VerificationURIComplete)
	}
	tok, err := oc.DeviceAccessToken(ctx, da)
	if err != nil {
		return fmt.Errorf("finish the device login: %w", err)
	}
	cfg.Issuer, cfg.ClientID, cfg.Scopes = cc.Issuer, cc.ClientID, cc.Scopes
	return keepIDToken(cfg, tok)
}

func oauthConfig(ctx context.Context, issuer, clientID string, scopes []string) (*oauth2.Config, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover the identity provider: %w", err)
	}
	return &oauth2.Config{ClientID: clientID, Endpoint: p.Endpoint(), Scopes: scopes}, nil
}

func keepIDToken(cfg *Config, tok *oauth2.Token) error {
	id, ok := tok.Extra("id_token").(string)
	if !ok {
		return errors.New("the identity provider sent no ID token")
	}
	cfg.Token, cfg.Expiry = id, tok.Expiry
	if tok.RefreshToken != "" {
		cfg.RefreshToken = tok.RefreshToken
	}
	return nil
}

// token returns the bearer token. It renews an ID token that ends in less than a minute.
func (a *app) token(ctx context.Context) (string, error) {
	c := a.cfg
	if c.Token == "" {
		return "", errors.New("log in first: chronicle login --server <URL>")
	}
	if c.Expiry.IsZero() || time.Until(c.Expiry) > time.Minute {
		return c.Token, nil
	}
	if c.RefreshToken == "" {
		return "", errors.New("the login has ended: chronicle login again")
	}
	oc, err := oauthConfig(ctx, c.Issuer, c.ClientID, c.Scopes)
	if err != nil {
		return "", err
	}
	tok, err := oc.TokenSource(ctx, &oauth2.Token{RefreshToken: c.RefreshToken}).Token()
	if err != nil {
		return "", fmt.Errorf("renew the login (chronicle login again): %w", err)
	}
	if err := keepIDToken(c, tok); err != nil {
		return "", err
	}
	return c.Token, c.save(a.configPath)
}
