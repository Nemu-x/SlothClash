package main

import "testing"

func TestFitWindowToWorkArea(t *testing.T) {
	def := windowSize{Width: 1100, Height: 720, MinWidth: 960, MinHeight: 640}
	cases := []struct {
		name         string
		workW, workH int
		want         windowSize
	}{
		{"big screen unchanged", 2560, 1392, def},
		{"unknown work area unchanged", 0, 0, def},
		{"1920x1080 at 150%", 1280, 688, windowSize{1100, 688, 960, 640}},
		{"1366x768 at 125%", 1092, 576, windowSize{1092, 576, 960, 576}},
		{"tiny", 800, 500, windowSize{800, 500, 800, 500}},
	}
	for _, c := range cases {
		if got := fitWindowToWorkArea(def, c.workW, c.workH); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
