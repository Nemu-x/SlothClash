package main

import (
	"strings"
)

// Pure helpers for the Linux service path, kept free of build tags so they are
// unit-tested on every platform (there is no Linux box in the dev loop; the
// tagged code in ipc_sloth_linux.go / service_install_linux.go is only
// vet-checked cross-platform).

// parseSystemctlShowState parses `systemctl show -p LoadState,ActiveState`
// output ("LoadState=loaded\nActiveState=active\n"). Unknown / "not-found"
// means the unit does not exist.
func parseSystemctlShowState(out string) (loaded, active bool) {
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(strings.ToLower(v))
		switch strings.TrimSpace(k) {
		case "LoadState":
			loaded = v == "loaded"
		case "ActiveState":
			active = v == "active" || v == "reloading" || v == "activating"
		}
	}
	return loaded, active
}

// linuxElevatedInstallArgv is the exact command used to run the service
// installer as root through polkit: `pkexec <installer> [--core-sha256 <pins>]`.
// pkexec needs an absolute program path and gives the child a clean
// environment; the installer resolves the invoking user from PKEXEC_UID (for
// the unit's Group=) and takes the core pins from argv, so nothing else has to
// survive the environment reset.
func linuxElevatedInstallArgv(pkexecPath, installerPath, coreHashes string) []string {
	argv := []string{pkexecPath, installerPath}
	if h := strings.TrimSpace(coreHashes); h != "" {
		argv = append(argv, "--core-sha256", h)
	}
	return argv
}

// linuxManualInstallCommand is what we show when pkexec is unavailable or
// refused: the same thing under sudo, copy-paste ready.
func linuxManualInstallCommand(installerPath, coreHashes string) string {
	var b strings.Builder
	b.WriteString("sudo ")
	b.WriteString(shellQuote(installerPath))
	if h := strings.TrimSpace(coreHashes); h != "" {
		b.WriteString(" --core-sha256 ")
		b.WriteString(shellQuote(h))
	}
	return b.String()
}

// explainPkexecExit maps polkit's documented exit codes to a sentence the
// user can act on. 126 = dialog dismissed, 127 = authentication failed /
// not authorised; anything else is the installer's own failure.
func explainPkexecExit(code int) string {
	switch code {
	case 126:
		return "the authorisation dialog was dismissed"
	case 127:
		return "authorisation failed or polkit refused (is a polkit agent running in this session?)"
	default:
		return ""
	}
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '/' || r == '.' || r == '-' || r == '_' || r == ',' || r == ':') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
