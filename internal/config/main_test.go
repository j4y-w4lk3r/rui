package config

import (
	"os"
	"testing"
)

// TestMain: tests never ask the real keyhole for the OpenBao token (it would wait for a touch).
func TestMain(m *testing.M) {
	os.Setenv("RUI_NO_KEYHOLE", "1")
	os.Setenv("RUI_BAO_TOKEN_FILE", os.DevNull+".missing")
	os.Exit(m.Run())
}
