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
		Title: "Sloth Clash",
		// 1100x720 leaves comfortable margins on 1366x768 laptops and on
		// scaled-up 4K monitors (150%/175% DPI) alike. The previous 1200x820
		// covered ~88%×107% of a 1366x768 screen which made the window taller
		// than the viewport on default Windows scaling. Min* values stay
		// the same so the rules editor and YAML modals still fit.
		Width:     1100,
		Height:    720,
		MinWidth:  960,
		MinHeight: 640,
		Frameless:        false,
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
