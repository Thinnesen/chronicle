package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// app holds the global flags and the clients of one command run.
type app struct {
	configPath string
	server     string
	namespace  string
	out        io.Writer
	cfg        *Config
}

// NewCommand returns the root command of the CLI.
func NewCommand() *cobra.Command {
	a := &app{out: os.Stdout}
	root := &cobra.Command{
		Use:           "chronicle",
		Short:         "Run and watch chronicle workflows",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.out = cmd.OutOrStdout()
			cfg, err := loadConfig(a.configPath)
			a.cfg = cfg
			return err
		},
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", defaultConfigPath(), "The configuration file.")
	root.PersistentFlags().StringVar(&a.server, "server", "", "The URL of the chronicle server. The default comes from the login.")
	root.PersistentFlags().StringVarP(&a.namespace, "namespace", "n", "", "The namespace. The default comes from the login.")
	root.AddCommand(a.loginCommand(), a.submitCommand(), a.listCommand(), a.getCommand(), a.watchCommand(), a.logsCommand(),
		a.deleteCommand(), a.approveCommand(), a.namespacesCommand())
	for _, c := range a.controlCommands() {
		root.AddCommand(c)
	}
	return root
}

// Execute runs the CLI and returns the exit code.
func Execute() int {
	if err := NewCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", errorText(err))
		return 1
	}
	return 0
}

// errorText removes the Connect code prefix from an error for people.
func errorText(err error) string {
	if ce, ok := errors.AsType[*connect.Error](err); ok {
		return ce.Message()
	}
	return err.Error()
}

func (a *app) ns() (string, error) {
	switch {
	case a.namespace != "":
		return a.namespace, nil
	case os.Getenv("CHRONICLE_NAMESPACE") != "":
		return os.Getenv("CHRONICLE_NAMESPACE"), nil
	case a.cfg.Namespace != "":
		return a.cfg.Namespace, nil
	}
	return "", errors.New("give a namespace with -n, $CHRONICLE_NAMESPACE or chronicle login --namespace")
}

func (a *app) serverURL() (string, error) {
	if a.server != "" {
		return a.server, nil
	}
	if a.cfg.Server != "" {
		return a.cfg.Server, nil
	}
	return "", errors.New("log in first: chronicle login --server <URL>")
}

// httpClient returns an HTTP client that sends the bearer token of the login.
func (a *app) httpClient(ctx context.Context) (*http.Client, error) {
	token, err := a.token(ctx)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: 0, Transport: bearer{token: token, next: http.DefaultTransport}}, nil
}

type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func (a *app) workflows(ctx context.Context) (chroniclev1connect.WorkflowServiceClient, error) {
	url, err := a.serverURL()
	if err != nil {
		return nil, err
	}
	hc, err := a.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	return chroniclev1connect.NewWorkflowServiceClient(hc, url), nil
}

// timeout returns a context for one short call.
func timeout(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), time.Minute)
}
