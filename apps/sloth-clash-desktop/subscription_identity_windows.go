//go:build windows

package main

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func rawStableMachineIDPlatform() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.READ)
	if err != nil {
		return ""
	}
	defer k.Close()
	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return ""
	}
	return "machineguid:" + strings.ToLower(guid)
}

func hostOSVersionLabelPlatform() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.READ)
	if err != nil {
		return "windows"
	}
	defer k.Close()

	product, _, _ := k.GetStringValue("ProductName")
	display, _, _ := k.GetStringValue("DisplayVersion")
	build, _, _ := k.GetStringValue("CurrentBuild")
	product = strings.TrimSpace(product)
	display = strings.TrimSpace(display)
	build = strings.TrimSpace(build)

	switch {
	case product != "" && display != "" && build != "":
		return fmt.Sprintf("%s %s (%s)", product, display, build)
	case product != "" && build != "":
		return fmt.Sprintf("%s (%s)", product, build)
	case display != "" && build != "":
		return fmt.Sprintf("%s (%s)", display, build)
	case product != "":
		return product
	default:
		return "windows"
	}
}

func hostDeviceModelLabelPlatform() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.READ)
	if err != nil {
		return "PC"
	}
	defer k.Close()
	manuf, _, _ := k.GetStringValue("SystemManufacturer")
	prod, _, _ := k.GetStringValue("SystemProductName")
	manuf = strings.TrimSpace(manuf)
	prod = strings.TrimSpace(prod)
	if manuf != "" && prod != "" {
		return manuf + " " + prod
	}
	if prod != "" {
		return prod
	}
	return "PC"
}
