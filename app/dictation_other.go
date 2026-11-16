//go:build !windows

package app

import "errors"

func winVoiceTyping() error { return errors.New("not Windows") }
