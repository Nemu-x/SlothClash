//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// linuxServiceStagingDir is where the embedded installer + service binary are
// unpacked before elevation. Not a temp dir on purpose: /tmp is commonly
// mounted noexec (pkexec would fail with EACCES), and when polkit is not
// available we hand the user a `sudo …` command that must still point at an
// existing file after this call returns.
func linuxServiceStagingDir() (string, error) {
	root, err := slothDataRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "service-install")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// installServiceElevatedLinux runs the Rust installer as root through polkit.
// The installer copies the staged service binary into /usr/local/lib/sloth-clash,
// writes the systemd unit (Group= the invoking user's primary group, resolved
// from PKEXEC_UID; Environment=SLOTH_CLASH_CORE_SHA256 from --core-sha256) and
// enables it. Returns the combined output plus an error whose text already
// tells the user what to do when polkit was the problem.
func installServiceElevatedLinux(installPath, workDir, coreHashes string) ([]byte, error) {
	_ = os.Chmod(installPath, 0o755)
	// The staged service binary next to the installer must be readable by root
	// for the copy — it is, we just created it — nothing to do.

	pkexec, err := exec.LookPath("pkexec")
	if err != nil {
		return nil, fmt.Errorf(
			"pkexec (polkit) is not available, so the installer cannot ask for authorisation. "+
				"Run it once from a terminal instead:\n\n    %s\n\nthen press Install service again to refresh the status.",
			linuxManualInstallCommand(installPath, coreHashes),
		)
	}

	argv := linuxElevatedInstallArgv(pkexec, installPath, coreHashes)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workDir
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return out, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if why := explainPkexecExit(exitErr.ExitCode()); why != "" {
			return out, fmt.Errorf(
				"%s. If this keeps happening, run it from a terminal:\n\n    %s",
				why,
				linuxManualInstallCommand(installPath, coreHashes),
			)
		}
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = runErr.Error()
	}
	return out, errors.New(msg)
}
