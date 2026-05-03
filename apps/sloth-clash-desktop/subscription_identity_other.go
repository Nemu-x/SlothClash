//go:build !windows && !darwin && !linux

package main

import (
	"fmt"
	"runtime"
)

func rawStableMachineIDPlatform() string {
	return ""
}

func hostOSVersionLabelPlatform() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}

func hostDeviceModelLabelPlatform() string {
	return runtime.GOARCH
}
