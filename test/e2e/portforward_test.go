//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var forwardLine = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+)`)

// portForward forwards a free local port to a Service and returns the local address. It stops after the spec.
func portForward(namespace, service string, port int) string {
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	cmd := exec.CommandContext(ctx, "kubectl", "port-forward", "-n", namespace, "svc/"+service, fmt.Sprintf(":%d", port))
	out, err := cmd.StdoutPipe()
	Expect(err).NotTo(HaveOccurred())
	Expect(cmd.Start()).To(Succeed())
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		if m := forwardLine.FindStringSubmatch(lines.Text()); m != nil {
			go func() {
				for lines.Scan() {
				}
			}()
			return "127.0.0.1:" + m[1]
		}
	}
	Fail("kubectl port-forward did not start for " + service)
	return ""
}

// clusterClient returns an HTTP client that reaches in-cluster host names through local port-forwards.
func clusterClient(routes map[string]string) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if local, ok := routes[addr]; ok {
			addr = local
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second}
}
