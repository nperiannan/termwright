package main

import (
	"runtime"

	"github.com/termwright/termwright/internal/macwin"
)

// StartDictation starts the system's dictation in the focused text field - the AI chat's microphone button:
// macOS Dictation, Windows voice typing (Win+H). It returns a hint when the system has none Termwright can start.
func (a *App) StartDictation() string {
	switch runtime.GOOS {
	case "darwin":
		if macwin.StartDictation() == nil {
			return ""
		}
		return "Turn on Dictation in System Settings → Keyboard, then press the microphone key (or Fn twice) in the message box."
	case "windows":
		if winVoiceTyping() == nil {
			return ""
		}
		return "Press Windows+H in the message box for voice typing."
	}
	return "This system has no dictation Termwright can start; a dictation tool of your desktop types into the message box too."
}
