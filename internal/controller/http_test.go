package controller

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Thinnesen/chronicle/internal/executor"
)

var _ = Describe("http templates", func() {
	ctx := context.Background()

	It("runs an http template without an API token, with a header from a Secret", func() {
		key := newWorkflow(ctx, `
entrypoint: main
serviceAccountName: deployer
templates:
- name: main
  http:
    url: "https://chat.example.com/{{workflow.name}}"
    method: POST
    headers: [{name: X-A, value: a}, {name: Authorization, valueFrom: {secretKeyRef: {name: chat, key: token}}}]
    body: hi
`)
		wf := reconcileWith(ctx, testReconciler(), key)
		pod := podsOf(ctx, wf)[0]
		Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
		Expect(pod.Spec.InitContainers).To(BeEmpty())
		main := pod.Spec.Containers[0]
		Expect(main.Image).To(Equal(testExecutorImage))
		Expect(main.Command).To(Equal([]string{"/chronicle-exec", "http"}))
		var spec executor.HTTPSpec
		Expect(json.Unmarshal([]byte(main.Env[0].Value), &spec)).To(Succeed())
		Expect(spec.URL).To(Equal("https://chat.example.com/" + wf.Name))
		Expect(spec.Headers).To(Equal([]executor.HTTPHeaderSpec{{Name: "X-A", Value: "a"}, {Name: "Authorization", Env: "CHRONICLE_HEADER_1"}}))
		Expect(main.Env[1].Name).To(Equal("CHRONICLE_HEADER_1"))
		Expect(main.Env[1].ValueFrom.SecretKeyRef.Name).To(Equal("chat"))
	})
})
