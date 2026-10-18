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
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Thinnesen/chronicle/test/utils"
)

const (
	serverHost = "chronicle-server.chronicle-system.svc.cluster.local:8080"
	dexHost    = "dex.dex.svc.cluster.local:5556"
	loginNS    = "chronicle-e2e-login"
)

var formAction = regexp.MustCompile(`<form[^>]*method="post"[^>]*action="([^"]+)"`)

// loginSpecs logs in through Dex like a browser and calls the API with the session. The Manager suite calls it.
func loginSpecs() {
	Context("Login", Ordered, func() {
		BeforeAll(func() {
			_, err := utils.Run(exec.Command("kubectl", "create", "ns", loginNS))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/dex.yaml"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "dex", "deploy/dex", "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "restart", "-n", "chronicle-system", "deploy/chronicle-server"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "chronicle-system", "deploy/chronicle-server", "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", "dex", loginNS, "--wait=false"))
		})

		It("logs in through Dex and uses the API with the RBAC of the user", func() {
			client := clusterClient(map[string]string{
				serverHost: portForward("chronicle-system", "chronicle-server", 8080),
				dexHost:    portForward("dex", "dex", 5556),
			})
			client.Jar, _ = cookiejar.New(nil)

			By("opening the login page of Dex")
			var page string
			Eventually(func(g Gomega) {
				resp, err := client.Get("http://" + serverHost + "/auth/login")
				g.Expect(err).NotTo(HaveOccurred())
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				page = string(body)
				g.Expect(formAction.FindStringSubmatch(page)).NotTo(BeNil(), "no login form in %s", page)
			}).WithTimeout(2 * time.Minute).WithPolling(3 * time.Second).Should(Succeed())

			By("sending the password")
			action := html.UnescapeString(formAction.FindStringSubmatch(page)[1])
			resp, err := client.PostForm("http://"+dexHost+action, url.Values{"login": {"alice@example.com"}, "password": {"password"}})
			Expect(err).NotTo(HaveOccurred())
			_ = resp.Body.Close()

			By("reading the session")
			resp, err = client.Get("http://" + serverHost + "/auth/me")
			Expect(err).NotTo(HaveOccurred())
			var me map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&me)).To(Succeed())
			_ = resp.Body.Close()
			Expect(me["user"]).To(Equal("oidc:alice@example.com"))
			csrf := me["csrfToken"].(string)

			list := func() int {
				req, _ := http.NewRequest(http.MethodPost, "http://"+serverHost+"/chronicle.v1.WorkflowService/ListWorkflows",
					strings.NewReader(`{"namespace":"`+loginNS+`"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-CSRF-Token", csrf)
				resp, err := client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				_ = resp.Body.Close()
				return resp.StatusCode
			}
			By("calling the API without a RoleBinding")
			Expect(list()).To(Equal(http.StatusForbidden))

			By("calling the API with a RoleBinding")
			_, err = utils.Run(exec.Command("kubectl", "create", "rolebinding", "alice-view", "-n", loginNS,
				"--clusterrole=chronicle-workflow-viewer-role", "--user=oidc:alice@example.com"))
			Expect(err).NotTo(HaveOccurred())
			Eventually(list).WithTimeout(30 * time.Second).Should(Equal(http.StatusOK))
		})
	})
}
