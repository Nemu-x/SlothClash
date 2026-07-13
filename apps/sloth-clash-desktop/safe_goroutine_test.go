package main

import (
	"sync"
	"testing"
)

// A panic inside safeGo must be contained (recovered), not propagate/crash.
func TestSafeGoRecoversPanic(t *testing.T) {
	a := &App{}
	var wg sync.WaitGroup
	wg.Add(1)
	a.safeGo("test", func() {
		defer wg.Done()
		panic("boom")
	})
	wg.Wait() // if the panic weren't recovered, the test process would crash
}
