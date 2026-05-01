package clipboard

import "testing"

func TestCopyEmpty(t *testing.T) {
	if err := Copy(""); err != nil {
		t.Fatalf("Copy(\"\"): %v", err)
	}
}
