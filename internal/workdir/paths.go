package workdir

import (
	"os"
	"path/filepath"
	"strings"
)

// NormalizeSandboxPath rewrites AMA magic paths to workdir-relative paths.
// Mirrors harness/oma_adapter/sandbox_paths.py and LocalSubprocessSandbox.
func NormalizeSandboxPath(sandboxPath string) string {
	normalised := sandboxPath
	if strings.HasPrefix(normalised, "/mnt/session/outputs/") ||
		normalised == "/mnt/session/outputs" {
		if rootMountExists("/mnt/session/outputs") {
			return normalised
		}
		if normalised == "/mnt/session/outputs" {
			return ".mnt/session/outputs"
		}
		return ".mnt/session/outputs/" + normalised[len("/mnt/session/outputs/"):]
	}
	if strings.HasPrefix(normalised, "/mnt/session/uploads/") ||
		normalised == "/mnt/session/uploads" {
		if rootMountExists("/mnt/session/uploads") {
			return normalised
		}
		if normalised == "/mnt/session/uploads" {
			return "mnt/session/uploads"
		}
		return "mnt/session/uploads/" + normalised[len("/mnt/session/uploads/"):]
	}
	if strings.HasPrefix(normalised, "/mnt/user-data/") ||
		normalised == "/mnt/user-data" {
		if rootMountExists("/mnt/user-data") {
			return normalised
		}
		if normalised == "/mnt/user-data" {
			return "mnt/user-data"
		}
		return "mnt/user-data/" + normalised[len("/mnt/user-data/"):]
	}
	if strings.HasPrefix(normalised, "/mnt/memory/") ||
		normalised == "/mnt/memory" {
		if rootMountExists("/mnt/memory") {
			return normalised
		}
		if normalised == "/mnt/memory" {
			return ".mnt/memory"
		}
		return ".mnt/memory/" + normalised[len("/mnt/memory/"):]
	}
	if strings.HasPrefix(normalised, "/workspace/") {
		return normalised[len("/workspace/"):]
	}
	if normalised == "/workspace" {
		return ""
	}
	if strings.HasPrefix(normalised, "/") {
		return normalised[1:]
	}
	return normalised
}

// ResolveSandboxPath resolves a sandbox path inside workdir.
func ResolveSandboxPath(workdirPath, sandboxPath string) (string, error) {
	if err := validateSessionPath(sandboxPath); err != nil {
		return "", err
	}
	rel := NormalizeSandboxPath(sandboxPath)
	candidate := filepath.Join(workdirPath, filepath.FromSlash(rel))
	absWorkdir, err := filepath.Abs(workdirPath)
	if err != nil {
		return "", err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	absWorkdir, err = filepath.EvalSymlinks(absWorkdir)
	if err != nil {
		absWorkdir, _ = filepath.Abs(workdirPath)
	}
	absCandidate, err = filepath.EvalSymlinks(absCandidate)
	if err != nil {
		return "", err
	}
	if absCandidate != absWorkdir &&
		!strings.HasPrefix(absCandidate, absWorkdir+string(os.PathSeparator)) {
		return "", errPathEscapesWorkdir
	}
	return absCandidate, nil
}

func rootMountExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func validateSessionPath(path string) error {
	if strings.Contains(path, "..") {
		return errInvalidSandboxPath
	}
	return nil
}

var (
	errPathEscapesWorkdir = errInvalidSandboxPath
	errInvalidSandboxPath = &sandboxPathError{msg: "invalid sandbox path"}
)

type sandboxPathError struct {
	msg string
}

func (e *sandboxPathError) Error() string {
	return e.msg
}
