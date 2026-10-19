// Command fanout builds a fan-out workflow. It prints the YAML, or submits the run with -server.
//
//	go run ./sdk/go/examples/fanout | kubectl create -n team-a -f -
//	go run ./sdk/go/examples/fanout -server https://chronicle.example.com -namespace team-a -token-file token
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Thinnesen/chronicle/sdk/go/chronicle"
)

func main() {
	server := flag.String("server", "", "the URL of the chronicle server; without it, the command prints the YAML")
	namespace := flag.String("namespace", "default", "the namespace of the run")
	tokenFile := flag.String("token-file", "", "a file with an OIDC ID token or a service account token")
	flag.Parse()
	if err := run(*server, *namespace, *tokenFile); err != nil {
		fmt.Fprintln(os.Stderr, "fanout:", err)
		os.Exit(1)
	}
}

func run(server, namespace, tokenFile string) error {
	w := build()
	if server == "" {
		out, err := w.YAML()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(out)
		return err
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return fmt.Errorf("read the token: %w", err)
	}
	c, err := chronicle.NewClient(server, strings.TrimSpace(string(token)))
	if err != nil {
		return err
	}
	name, err := c.Submit(context.Background(), namespace, w)
	if err != nil {
		return err
	}
	fmt.Printf("Started %s/%s\n", namespace, name)
	return nil
}

// build makes one "greet" task for each region, then a "done" task after all of them.
func build() *chronicle.Workflow {
	w := chronicle.New("fanout-")
	greeting := w.Param("greeting", "hello")

	greet := chronicle.Container("greet", "busybox:1.37", "echo")
	greet.Args(greet.Input("text"))

	main := chronicle.DAG("main")
	each := main.Task("greet", greet).Arg("text", greeting+" from {{item}}").WithItems("eu", "us", "asia")
	main.Task("done", greet).After(each).Arg("text", "all regions done")
	return w.Entrypoint(main)
}
