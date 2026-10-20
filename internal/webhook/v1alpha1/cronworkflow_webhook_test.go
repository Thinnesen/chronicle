package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("CronWorkflow validation", func() {
	newCron := func(spec string) *chroniclev1alpha1.CronWorkflow {
		cw := &chroniclev1alpha1.CronWorkflow{Name: "cw-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(spec), &cw.Spec)).To(Succeed())
		return cw
	}
	const wf = "\nworkflowSpec: {entrypoint: c, templates: [" + container + "]}"

	It("accepts a valid CronWorkflow and rejects a bad update", func() {
		cw := newCron("schedules: ['*/5 * * * *']\ntimezone: Europe/Berlin" + wf)
		Expect(k8sClient.Create(ctx, cw)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, cw)
		cw.Spec.Schedules = []string{"every day"}
		err := k8sClient.Update(ctx, cw)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.schedules"))
	})

	DescribeTable("rejects",
		func(spec string, text string) { expectRejected(newCron(spec), text) },
		Entry("a bad schedule", "schedules: ['every day']"+wf, "spec.schedules"),
		Entry("a bad time zone", "schedules: ['@daily']\ntimezone: Mars/Base"+wf, "unknown time zone"),
		Entry("a bad workflow spec", "schedules: ['@daily']\nworkflowSpec: {entrypoint: nope, templates: ["+container+"]}", "spec.workflowSpec.entrypoint"),
	)
})
