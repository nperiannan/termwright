//go:build !windows

package main

import "errors"

func winVoiceTyping() error { return errors.New("not Windows") }
