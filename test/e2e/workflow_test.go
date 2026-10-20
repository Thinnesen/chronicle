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

		phaseOf := func(name string) func(g Gomega) string {
			return func(g Gomega) string {
				out, err := utils.Run(exec.Command("kubectl", "get", "workflow", name, "-n", workflowNamespace, "-o", "jsonpath={.status.phase}"))
				g.Expect(err).NotTo(HaveOccurred())
				return out
			}
		}

		It("waits for an approval and passes its parameters on", func() {
			_, err := utils.Run(exec.Command("kubectl", "apply", "-n", workflowNamespace, "-f", "test/e2e/testdata/approval.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Eventually(phaseOf("approval")).WithTimeout(time.Minute).Should(Equal("Running"))
			Consistently(phaseOf("approval")).WithTimeout(10 * time.Second).Should(Equal("Running"))
			_, err = utils.Run(exec.Command("kubectl", "patch", "workflow", "approval", "-n", workflowNamespace, "--type", "merge",
				"-p", `{"spec":{"approvals":[{"node":"approval[0].gate","decision":"Approve","parameters":[{"name":"env","value":"prod"}]}]}}`))
			Expect(err).NotTo(HaveOccurred())
			Eventually(phaseOf("approval")).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Equal("Succeeded"))
		})

		It("starts a CronWorkflow run from a WorkflowTemplate", func() {
			_, err := utils.Run(exec.Command("kubectl", "apply", "-n", workflowNamespace, "-f", "test/e2e/testdata/cron.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				out, err := utils.Run(exec.Command("kubectl", "get", "workflows", "-n", workflowNamespace,
					"-l", "chronicle.thinnesen.com/cron-workflow=every-minute", "-o", "jsonpath={.items[*].status.phase}"))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("Succeeded"))
			}).WithTimeout(4 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
			_, err = utils.Run(exec.Command("kubectl", "delete", "cronworkflow", "every-minute", "-n", workflowNamespace))
			Expect(err).NotTo(HaveOccurred())
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
			Entry("a resource template that is limited to its service account", "resource.yaml", "resource", "Failed", `step "denied" failed`),
			Entry("a retry, a timeout and an exit handler", "retry-timeout.yaml", "retry-timeout", "Succeeded", ""),
			Entry("a fan-out with a parallelism limit and a mutex", "limited.yaml", "limited", "Succeeded", ""),
			Entry("a child run whose output flows to the next step", "child.yaml", "child-parent", "Succeeded", ""),
		)
	})
}
