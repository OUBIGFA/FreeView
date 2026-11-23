package main

import (
	"fmt"
	"os"
	"sync"
)

// runBrowserWaitDesktop runs only in the test executable under deskrun.
// Each browser handoff stays pending until the app closes, so the script
// can verify both native windows remain responsive for the whole wait.
func runBrowserWaitDesktop(path string) {
	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	var mu sync.Mutex
	done := make(chan struct{})
	defer close(done)
	launchBrowser = func(url string) error {
		mu.Lock()
		_, err := fmt.Fprintln(file, url)
		mu.Unlock()
		if err != nil {
			return err
		}
		<-done
		return nil
	}
	main()
}
