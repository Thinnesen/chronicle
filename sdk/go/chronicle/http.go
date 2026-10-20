package chronicle

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// HTTP returns a template that sends one GET request from a Pod. A status from 200 to 299 is success, and
// the response body is the result. The URL can have placeholders.
func HTTP(name, url string) *Template {
	return &Template{t: v1alpha1.Template{Name: name, HTTP: &v1alpha1.HTTPTemplate{URL: url}}}
}

func (t *Template) http() *v1alpha1.HTTPTemplate {
	if t.t.HTTP == nil {
		t.errs = append(t.errs, fmt.Errorf("template %q is not an http template", t.t.Name))
		return &v1alpha1.HTTPTemplate{}
	}
	return t.t.HTTP
}

// Method sets the method of the request, for example "POST".
func (t *Template) Method(method string) *Template {
	t.http().Method = v1alpha1.HTTPMethod(method)
	return t
}

// Body sets the body of the request. It can have placeholders.
func (t *Template) Body(body string) *Template {
	t.http().Body = body
	return t
}

// Header adds a header to the request.
func (t *Template) Header(name, value string) *Template {
	h := t.http()
	h.Headers = append(h.Headers, v1alpha1.HTTPHeader{Name: name, Value: &value})
	return t
}

// HeaderFromSecret adds a header whose value is the key of a Secret in the namespace, for example a token.
func (t *Template) HeaderFromSecret(name, secret, key string) *Template {
	h := t.http()
	h.Headers = append(h.Headers, v1alpha1.HTTPHeader{Name: name, ValueFrom: &v1alpha1.HTTPHeaderSource{
		SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key},
	}})
	return t
}
