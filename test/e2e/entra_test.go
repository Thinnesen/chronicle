//go:build e2e

package e2e

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Thinnesen/chronicle/test/utils"
)

const (
	mockEntraHost  = "mock-entra.mock-entra.svc.cluster.local:8080"
	mockEntraImage = "example.com/mock-entra:e2e"
)

// entraSpecs logs in two users through a mock Entra ID. Each team sees only its own namespace.
func entraSpecs() {
	Context("Microsoft Entra ID", Ordered, func() {
		BeforeAll(func() {
			_, err := utils.Run(exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/mockentra.yaml"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "mock-entra", "deploy/mock-entra", "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "restart", "-n", "chronicle-system", "deploy/chronicle-server"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "chronicle-system", "deploy/chronicle-server", "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", "mock-entra", "team-red", "team-blue", "--wait=false"))
		})

		DescribeTable("shows each team only its namespaces",
			func(user, namespaces string) {
				client := clusterClient(map[string]string{
					serverHost:    portForward("chronicle-system", "chronicle-server", 8080),
					mockEntraHost: portForward("mock-entra", "mock-entra", 8080),
				})
				client.Jar, _ = cookiejar.New(nil)
				var page string
				Eventually(func(g Gomega) {
					resp, err := client.Get("http://" + serverHost + "/auth/login")
					g.Expect(err).NotTo(HaveOccurred())
					body, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					page = string(body)
					g.Expect(formAction.FindStringSubmatch(page)).NotTo(BeNil(), "no user form in %s", page)
				}).WithTimeout(2 * time.Minute).WithPolling(3 * time.Second).Should(Succeed())
				action := html.UnescapeString(formAction.FindStringSubmatch(page)[1])
				resp, err := client.PostForm("http://"+mockEntraHost+action, url.Values{"user": {user}})
				Expect(err).NotTo(HaveOccurred())
				_ = resp.Body.Close()

				resp, err = client.Get("http://" + serverHost + "/auth/me")
				Expect(err).NotTo(HaveOccurred())
				var me map[string]any
				Expect(json.NewDecoder(resp.Body).Decode(&me)).To(Succeed())
				_ = resp.Body.Close()
				Expect(me["user"]).To(Equal("entra:" + user + "-oid"))

				req, _ := http.NewRequest(http.MethodPost, "http://"+serverHost+"/chronicle.v1.NamespaceService/ListNamespaces", strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-CSRF-Token", me["csrfToken"].(string))
				resp, err = client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				var out struct {
					Namespaces []struct {
						Name string `json:"name"`
					} `json:"namespaces"`
				}
				Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
				_ = resp.Body.Close()
				var names []string
				for _, n := range out.Namespaces {
					names = append(names, n.Name)
				}
				Expect(strings.Join(names, ",")).To(Equal(namespaces))
			},
			Entry("a user with the group in the token", "ann", "team-red"),
			Entry("a user with a group overage", "bo", "team-blue"),
		)
	})
}
