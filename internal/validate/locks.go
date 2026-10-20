package validate

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// checkLock checks the lock of a template: only a template that does one piece of work can hold it, and only the lock
// of a run can skip.
func (c *checker) checkLock(t *v1alpha1.Template, path *field.Path) {
	if t.Synchronization == nil {
		return
	}
	if t.Container == nil && t.Script == nil && t.Resource == nil && t.HTTP == nil && t.Task == nil {
		c.errs = append(c.errs, field.Forbidden(path.Child("synchronization"), "only a container, script, resource, http or task template can hold a lock"))
	}
	if t.Synchronization.WhenBusy != "" {
		c.errs = append(c.errs, field.Forbidden(path.Child("synchronization", "whenBusy"), "only the lock of a run can have whenBusy"))
	}
}
