// Command worker runs the task "go-greet" of the e2e workflow test/e2e/testdata/tasks.yaml.
//
//	go run ./sdk/go/examples/worker -server https://chronicle.example.com -namespace team-a -token-file token
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Thinnesen/chronicle/sdk/go/chronicle"
)

func main() {
	server := flag.String("server", "", "the URL of the chronicle server")
	namespace := flag.String("namespace", "default", "the namespace of the tasks")
	tokenFile := flag.String("token-file", "", "a file with an OIDC ID token or a service account token")
	flag.Parse()
	if err := run(*server, *namespace, *tokenFile); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run(server, namespace, tokenFile string) error {
	// The client reads the file for each call, so a projected token that Kubernetes rotates keeps working.
	client, err := chronicle.NewClientFromTokenFile(server, tokenFile)
	if err != nil {
		return err
	}
	w := chronicle.NewWorker(client, namespace, chronicle.WorkerOptions{Concurrency: 2})
	w.Register("go-greet", func(_ context.Context, _ chronicle.TaskInfo, in map[string]string) (any, error) {
		return map[string]string{"greeting": "Go says hello to " + in["who"]}, nil
	})
	// SIGTERM and Ctrl-C stop the polling; running tasks get the grace period.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return w.Run(ctx)
}
