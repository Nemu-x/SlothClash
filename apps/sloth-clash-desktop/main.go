package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:build/resources
//go:embed all:build/sidecar
var bundledResources embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp(bundledResources)

	// Create application with options
	err := wails.Run(&options.App{
		Title:            "Sloth Clash",
		Width:            1024,
		Height:           768,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 18, G: 17, B: 16, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Windows: &windows.Options{
			Theme: windows.Dark,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "a3f2c8d1-4b5e-4f6a-9c0d-slothclash-desktop",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				app.OnSecondInstance(data)
			},
		},
		OnStartup:     app.startup,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
