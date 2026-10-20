package chronicle

import "testing"

func TestSkipWhenBusy(t *testing.T) {
	main := Container("c", "alpine", "true")
	spec, err := New("sync-").SkipWhenBusy().Mutex("sync").Entrypoint(main).Spec()
	if err != nil || spec.Synchronization.WhenBusy != "Skip" || spec.Synchronization.Mutex.Name != "sync" {
		t.Errorf("spec %+v, %v", spec, err)
	}
	if _, err := New("x-").SkipWhenBusy().Entrypoint(main).Spec(); err == nil {
		t.Error("SkipWhenBusy without a lock gave no error")
	}
}
