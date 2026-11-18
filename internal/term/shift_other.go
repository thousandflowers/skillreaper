//go:build !darwin

package term

// startKeyboardWatcher has nobody to ask off macOS: terminals there send
// Shift+arrows as their own sequences, and the plain key applies.
func startKeyboardWatcher() *keyboardWatcher { return nil }
