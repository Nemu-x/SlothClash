package main

// windowSize is the outer size of the main window in Wails logical pixels
// (Wails sizes the whole window, title bar and borders included).
type windowSize struct {
	Width, Height       int
	MinWidth, MinHeight int
}

// fitWindowToWorkArea shrinks the startup window (and, if needed, its
// minimum size) so it fits inside the monitor work area.
//
// Wails centres the window on the work area without clamping, so a window
// taller than the work area (1366x768 or 1920x1080 at 125-150% scaling, for
// example) gets a negative Y and its title bar - with the minimise/close
// buttons - ends up above the top edge of the screen where it can't be
// reached or dragged. workW/workH <= 0 means "unknown" and leaves the size
// unchanged.
func fitWindowToWorkArea(s windowSize, workW, workH int) windowSize {
	if workW > 0 && s.Width > workW {
		s.Width = workW
	}
	if workH > 0 && s.Height > workH {
		s.Height = workH
	}
	// The minimum must never exceed the chosen size, otherwise Wails clamps the
	// window back up past the work area.
	if s.MinWidth > s.Width {
		s.MinWidth = s.Width
	}
	if s.MinHeight > s.Height {
		s.MinHeight = s.Height
	}
	return s
}
