//go:build darwin && cgo && !slothtray

package main

import _ "embed"

//go:embed trayicons/mono.png
var darwinTrayMonoPNG []byte
