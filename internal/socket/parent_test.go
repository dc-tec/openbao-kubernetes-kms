package socket

import (
	"errors"
	"os"
	"testing"
)

func TestParentOwnerMustMatchProcess(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := validateParentOwner(info, os.Geteuid()); err != nil {
		t.Fatalf("accept current owner: %v", err)
	}
	if err := validateParentOwner(info, os.Geteuid()+1); !errors.Is(err, ErrUnsafeParent) {
		t.Fatalf("expected unsafe parent for another owner, got %v", err)
	}
}
