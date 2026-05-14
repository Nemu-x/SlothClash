//go:build darwin && cgo

package main

import _ "embed"

//go:embed trayicons/mono.png
var darwinTrayMonoPNG []byte
