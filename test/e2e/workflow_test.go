//go:build e2e

package e2e

import (
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Thinnesen/chronicle/test/utils"
)

// workflowNamespace enforces the restricted Pod security standard, so the test also checks the Pod hardening.
const workflowNamespace = "chronicle-e2e-workflows"

// workflowSpecs runs workflows on the deployed controller. The Manager suite calls it.
func workflowSpecs() {
	Context("Workflows", Ordered, func() {
		BeforeAll(func() {
			_, err := utils.Run(exec.Command("kubectl", "create", "ns", workflowNamespace))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "label", "ns", workflowNamespace, "pod-security.kubernetes.io/enforce=restricted"))
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", workflowNamespace, "--wait=false"))
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				out, _ := utils.Run(exec.Command("kubectl", "get", "workflows,pods", "-n", workflowNamespace, "-o", "yaml"))
				_, _ = GinkgoWriter.Write([]byte(out))
			}
		})

		DescribeTable("run to the expected phase",
			func(file, name, phase, message string) {
				_, err := utils.Run(exec.Command("kubectl", "apply", "-n", workflowNamespace, "-f", "test/e2e/testdata/"+file))
				Expect(err).NotTo(HaveOccurred())
				Eventually(func(g Gomega) {
					out, err := utils.Run(exec.Command("kubectl", "get", "workflow", name, "-n", workflowNamespace,
						"-o", "jsonpath={.status.phase}|{.status.message}"))
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(Equal(phase + "|" + message))
				}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
			},
			Entry("a DAG with outputs and fan-out", "dag-succeeds.yaml", "dag-succeeds", "Succeeded", ""),
			Entry("a failing DAG", "dag-fails.yaml", "dag-fails", "Failed", `task "boom" failed`),
			Entry("a retry, a timeout and an exit handler", "retry-timeout.yaml", "retry-timeout", "Succeeded", ""),
		)
	})
}
