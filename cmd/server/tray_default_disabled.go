//go:build !tray

package main

// defaultTrayMode keeps the default headless build from starting the tray
// unless the user explicitly passes --tray.
var defaultTrayMode = false
