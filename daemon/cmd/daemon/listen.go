package main

import "fmt"

// describeListenError spells out a port conflict. The daemon is supervised and
// restarted on exit, so an unexplained bind failure reads as a crash loop.
func describeListenError(addr string, err error) string {
	if isAddrInUse(err) {
		return fmt.Sprintf("cannot listen on %s: another daemon already owns it, so this one is exiting; stop the other daemon first", addr)
	}
	return fmt.Sprintf("listen on %s: %v", addr, err)
}
