package discover

// RaiseFileLimit does nothing on Windows (no per-process socket limit to lift).
func RaiseFileLimit() {}

func fileLimit() int { return 0 }
