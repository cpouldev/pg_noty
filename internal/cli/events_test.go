package cli

import "testing"

func TestRetrySelectorsAreBoundedAndMutuallyExclusive(t *testing.T) {
	if retryBatchLimit != 1000 {
		t.Fatalf("retry batch limit = %d", retryBatchLimit)
	}
	if _, err := retrySelector(1, true, "order", "dead"); err == nil {
		t.Fatal("mixed selector accepted")
	}
	if _, err := retrySelector(0, false, "", ""); err == nil {
		t.Fatal("empty selector accepted")
	}
	if selector, err := retrySelector(1, true, "", ""); err != nil || selector.ID == nil {
		t.Fatalf("id selector = %+v, err=%v", selector, err)
	}
	if selector, err := retrySelector(0, false, "order", "dead"); err != nil || selector.Listener != "order" {
		t.Fatalf("listener selector = %+v, err=%v", selector, err)
	}
}
