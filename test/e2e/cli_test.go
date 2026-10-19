//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Thinnesen/chronicle/test/utils"
)

const cliNS = "chronicle-e2e-cli"

// cliSpecs runs the chronicle CLI with a service account token. The Manager suite calls it.
func cliSpecs() {
	Context("CLI", Ordered, func() {
		var cli, config string

		BeforeAll(func() {
			cli = filepath.Join(GinkgoT().TempDir(), "chronicle")
			config = filepath.Join(GinkgoT().TempDir(), "config.json")
			for _, args := range [][]string{
				{"go", "build", "-o", cli, "./cmd/chronicle"},
				// The webhook must be ready, also when only these specs run.
				{"kubectl", "rollout", "status", "-n", "chronicle-system", "deploy/chronicle-controller-manager", "--timeout=3m"},
				{"kubectl", "create", "ns", cliNS},
				{"kubectl", "create", "serviceaccount", "ci", "-n", cliNS},
				{"kubectl", "create", "rolebinding", "ci-edit", "-n", cliNS, "--clusterrole=chronicle-workflow-editor-role", "--serviceaccount=" + cliNS + ":ci"},
				{"kubectl", "create", "role", "logs", "-n", cliNS, "--verb=get", "--resource=pods,pods/log"},
				{"kubectl", "create", "rolebinding", "ci-logs", "-n", cliNS, "--role=logs", "--serviceaccount=" + cliNS + ":ci"},
			} {
				_, err := utils.Run(exec.Command(args[0], args[1:]...))
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterAll(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", cliNS, "--wait=false"))
		})

		// server is a port-forward of the current spec. A port-forward ends with its spec.
		var server string
		BeforeEach(func() {
			_, err := utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "chronicle-system", "deploy/chronicle-server", "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())
			server = "http://" + portForward("chronicle-system", "chronicle-server", 8080)
		})

		chronicle := func(args ...string) (string, error) {
			return utils.Run(exec.Command(cli, append([]string{"--config", config, "--server", server}, args...)...))
		}

		It("logs in with a service account token and runs a workflow", func() {
			token, err := utils.Run(exec.Command("kubectl", "create", "token", "ci", "-n", cliNS, "--audience=chronicle"))
			Expect(err).NotTo(HaveOccurred())

			login := exec.Command(cli, "--config", config, "login", "--server", server, "-n", cliNS, "--token-file", "-")
			login.Stdin = strings.NewReader(token)
			_, err = utils.Run(login)
			Expect(err).NotTo(HaveOccurred())

			out, err := chronicle("submit", "-f", "test/e2e/testdata/dag-succeeds.yaml", "--watch")
			Expect(err).NotTo(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("dag-succeeds Succeeded"))

			Eventually(func() (string, error) { return chronicle("logs", "dag-succeeds") }).
				WithTimeout(30 * time.Second).Should(ContainSubstring("hello world"))
			out, err = chronicle("get", "dag-succeeds")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("✔ check"))
			out, err = chronicle("list")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(MatchRegexp(`dag-succeeds\s+Succeeded`))
		})

		It("keeps a deleted run in the archive", func() {
			_, err := utils.Run(exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/postgres.yaml"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "chronicle-db", "deploy/postgres", "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())
			patch := `[{"op":"add","path":"/spec/template/spec/volumes/-","value":{"name":"archive","secret":{"secretName":"chronicle-archive"}}},` +
				`{"op":"add","path":"/spec/template/spec/containers/0/volumeMounts/-","value":{"name":"archive","mountPath":"/etc/chronicle/archive","readOnly":true}},` +
				`{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--archive-dsn-file=/etc/chronicle/archive/dsn"}]`
			for _, d := range []string{"chronicle-controller-manager", "chronicle-server"} {
				_, err = utils.Run(exec.Command("kubectl", "patch", "deployment", d, "-n", "chronicle-system", "--type", "json", "-p", patch))
				Expect(err).NotTo(HaveOccurred())
				_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "chronicle-system", "deploy/"+d, "--timeout=3m"))
				Expect(err).NotTo(HaveOccurred())
			}
			// The server restarted: forward its port again.
			server = "http://" + portForward("chronicle-system", "chronicle-server", 8080)

			_, err = utils.Run(exec.Command("kubectl", "apply", "-n", cliNS, "-f", "test/e2e/testdata/archived.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				_, err := utils.Run(exec.Command("kubectl", "get", "workflow", "archived", "-n", cliNS))
				return err != nil
			}).WithTimeout(3*time.Minute).WithPolling(3*time.Second).Should(BeTrue(), "the TTL must delete the run")

			out, err := chronicle("get", "archived")
			Expect(err).NotTo(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("Phase:     Succeeded"))
			out, err = chronicle("list")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(MatchRegexp(`archived\s+Succeeded`))
		})

		It("submits a workflow from the Go SDK", func() {
			token, err := utils.Run(exec.Command("kubectl", "create", "token", "ci", "-n", cliNS, "--audience=chronicle"))
			Expect(err).NotTo(HaveOccurred())
			tokenFile := filepath.Join(GinkgoT().TempDir(), "token")
			Expect(os.WriteFile(tokenFile, []byte(token), 0o600)).To(Succeed())
			out, err := utils.Run(exec.Command("go", "run", "./sdk/go/examples/fanout", "-server", server, "-namespace", cliNS, "-token-file", tokenFile))
			Expect(err).NotTo(HaveOccurred(), out)
			name := strings.TrimSpace(strings.TrimPrefix(out, "Started "+cliNS+"/"))
			out, err = chronicle("watch", name)
			Expect(err).NotTo(HaveOccurred(), out)
			Expect(out).To(ContainSubstring(name + " Succeeded"))
		})

		It("converts Argo examples and runs them", func() {
			const busybox = "busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e"
			for _, example := range []string{"hello-world", "steps", "dag-diamond", "output-parameter"} {
				data, err := os.ReadFile("internal/convert/testdata/argo/" + example + ".yaml")
				Expect(err).NotTo(HaveOccurred())
				// The examples use an image tag; the tests pin images by digest.
				pinned := strings.ReplaceAll(string(data), "image: busybox\n", "image: "+busybox+"\n")
				in := filepath.Join(GinkgoT().TempDir(), example+".yaml")
				Expect(os.WriteFile(in, []byte(pinned), 0o600)).To(Succeed())
				converted, err := utils.Run(exec.Command(cli, "convert", "--strict", "-f", in))
				Expect(err).NotTo(HaveOccurred(), converted)
				create := exec.Command("kubectl", "create", "-n", cliNS, "-o", "name", "-f", "-")
				create.Stdin = strings.NewReader(converted)
				out, err := utils.Run(create)
				Expect(err).NotTo(HaveOccurred(), out)
				name := strings.TrimPrefix(strings.TrimSpace(out), "workflow.chronicle.thinnesen.com/")
				out, err = chronicle("watch", name)
				Expect(err).NotTo(HaveOccurred(), "%s: %s", example, out)
			}
		})

		It("passes an artifact between steps and archives the logs", func() {
			_, err := utils.Run(exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/seaweedfs.yaml"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "apply", "-n", cliNS, "-f", "test/e2e/testdata/repository.yaml"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "-n", "chronicle-s3", "deploy/seaweedfs", "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())

			out, err := chronicle("submit", "-f", "test/e2e/testdata/artifacts.yaml", "--watch")
			Expect(err).NotTo(HaveOccurred(), out)
			Eventually(func() (string, error) {
				return utils.Run(exec.Command("kubectl", "get", "pods", "-n", cliNS, "-l", "chronicle.thinnesen.com/workflow=artifacts", "-o", "name"))
			}).WithTimeout(time.Minute).WithPolling(2*time.Second).Should(BeEmpty(), "the Pod GC must delete the Pods")
			out, err = chronicle("logs", "artifacts")
			Expect(err).NotTo(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("passed-between-steps"))
		})

		It("runs a task in a worker of each SDK", func() {
			// The task queue is in the archive database, which the archive spec configured.
			for _, args := range [][]string{
				{"kubectl", "create", "role", "task-worker", "-n", cliNS, "--verb=update", "--resource=workflows.chronicle.thinnesen.com/tasks"},
				{"kubectl", "create", "rolebinding", "ci-tasks", "-n", cliNS, "--role=task-worker", "--serviceaccount=" + cliNS + ":ci"},
			} {
				_, err := utils.Run(exec.Command(args[0], args[1:]...))
				Expect(err).NotTo(HaveOccurred())
			}
			token, err := utils.Run(exec.Command("kubectl", "create", "token", "ci", "-n", cliNS, "--audience=chronicle", "--duration=1h"))
			Expect(err).NotTo(HaveOccurred())
			tokenFile := filepath.Join(GinkgoT().TempDir(), "token")
			Expect(os.WriteFile(tokenFile, []byte(token), 0o600)).To(Succeed())
			workers := []*exec.Cmd{
				exec.Command("go", "run", "./sdk/go/examples/worker", "-server", server, "-namespace", cliNS, "-token-file", tokenFile),
				exec.Command("uv", "run", "--frozen", "--project", "sdk/python", "sdk/python/examples/worker.py",
					"--server", server, "--namespace", cliNS, "--token-file", tokenFile),
				exec.Command("cargo", "run", "--quiet", "--manifest-path", "sdk/rust/Cargo.toml", "--example", "worker", "--",
					server, cliNS, tokenFile),
			}
			for _, w := range workers {
				w.Stdout, w.Stderr = GinkgoWriter, GinkgoWriter
				// A process group, so that the stop signal also reaches the program that go run or cargo run starts.
				w.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				Expect(w.Start()).To(Succeed())
			}
			DeferCleanup(func() {
				for _, w := range workers {
					_ = syscall.Kill(-w.Process.Pid, syscall.SIGTERM)
					_ = w.Wait()
				}
			})

			out, err := chronicle("submit", "-f", "test/e2e/testdata/tasks.yaml", "--watch")
			Expect(err).NotTo(HaveOccurred(), out)
			values, err := utils.Run(exec.Command("kubectl", "get", "workflow", "tasks", "-n", cliNS,
				"-o", "jsonpath={.status.nodes}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(ContainSubstring("Rust says hello to Python says hello to Go says hello to chronicle"))
		})

		It("retries and re-runs a run", func() {
			jsonpath := func(name, path string) string {
				out, err := utils.Run(exec.Command("kubectl", "get", "workflow", name, "-n", cliNS, "-o", "jsonpath="+path))
				Expect(err).NotTo(HaveOccurred())
				return out
			}
			out, err := chronicle("submit", "-f", "test/e2e/testdata/param-exit.yaml", "--watch")
			Expect(err).To(HaveOccurred(), "the run must fail: %s", out)
			Expect(out).To(ContainSubstring("param-exit Failed"))

			By("retrying the failed node, which fails again")
			out, err = chronicle("retry", "param-exit", "--watch")
			Expect(err).To(HaveOccurred(), out)
			Expect(jsonpath("param-exit", "{.status.restarts}")).To(Equal("1"))
			pods, err := utils.Run(exec.Command("kubectl", "get", "pods", "-n", cliNS, "-o", "name"))
			Expect(err).NotTo(HaveOccurred())
			Expect(pods).To(MatchRegexp(`pod/param-exit-\d+-r1`))

			By("re-running with a changed parameter")
			out, err = chronicle("rerun", "param-exit", "-p", "code=0", "--watch")
			Expect(err).NotTo(HaveOccurred(), out)
			rerun := strings.Fields(strings.TrimPrefix(strings.SplitN(out, "\n", 2)[0], "Started "+cliNS+"/"))[0]

			By("retrying the new run from its second node")
			out, err = chronicle("retry", rerun, "--node", rerun+".second", "--watch")
			Expect(err).NotTo(HaveOccurred(), out)
			Expect(jsonpath(rerun, "{.status.phase}/{.status.restarts}")).To(Equal("Succeeded/1"))
		})
	})
}
