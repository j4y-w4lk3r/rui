package livebox

import (
	"errors"
	"testing"
)

func TestIsPermissionDenied(t *testing.T) {
	if !IsPermissionDenied(errors.New("NMC.Guest.get: Permission denied")) {
		t.Fatal("expected permission denied")
	}
	if IsPermissionDenied(errors.New("timeout")) {
		t.Fatal("unexpected permission denied")
	}
	if IsPermissionDenied(nil) {
		t.Fatal("nil is not permission denied")
	}
}
