package validate

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// checkChild checks the arguments of a child run. Each needs a value, which can have placeholders.
func (c *checker) checkChild(w *v1alpha1.ChildWorkflowTemplate, path *field.Path, sc scope) {
	args := path.Child("arguments")
	if len(w.Arguments.Artifacts) > 0 {
		c.errs = append(c.errs, field.Forbidden(args.Child("artifacts"), "a child run cannot get artifacts"))
	}
	for i, p := range w.Arguments.Parameters {
		pp := args.Child("parameters").Index(i)
		if p.Value == nil {
			c.errs = append(c.errs, field.Required(pp.Child("value"), "an argument of a child run needs a value"))
			continue
		}
		c.checkText(*p.Value, pp.Child("value"), sc, nil)
	}
}
