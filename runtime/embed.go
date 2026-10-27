package runtime

import (
	_ "embed"
	"os"
	"path/filepath"
)

// RuntimeHeader is embedded so installed compiler binaries can initialize the
// generated program runtime without depending on the source checkout.
//
//go:embed ube_runtime.h
var RuntimeHeader []byte

func EnsureRuntime() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	installDir := os.Getenv("UBE_INSTALL_DIR")
	if installDir == "" {
		installDir = filepath.Join(home, ".ube")
	}
	dir := filepath.Join(installDir, "runtime")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "ube_runtime.h"), RuntimeHeader, 0644); err != nil {
		return "", err
	}
	return dir, nil
}
