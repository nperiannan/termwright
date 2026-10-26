package main

import (
	"strings"

	"github.com/termwright/termwright/internal/smartcard"
)

// SmartCardInfo is the Smart Cards page of Global Options.
type SmartCardInfo struct {
	Available bool               `json:"available"` // this build supports PKCS#11
	Found     []string           `json:"found"`     // libraries installed in the usual places
	Reports   []smartcard.Report `json:"reports"`   // cards and keys per library in use
	LoggedIn  int                `json:"loggedIn"`  // cards with the PIN entered
}

func (a *App) smartCardLibraries() []string {
	var out []string
	for _, l := range a.cfg.Get().SmartCardLibraries {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		out = smartcard.DefaultLibraries()
	}
	return out
}

// SmartCards lists the cards and their keys for the given libraries (none =
// the configured ones or those found in the usual places). No PIN is needed.
func (a *App) SmartCards(libs []string) SmartCardInfo {
	var use []string
	for _, l := range libs {
		if l = strings.TrimSpace(l); l != "" {
			use = append(use, l)
		}
	}
	if len(use) == 0 {
		use = a.smartCardLibraries()
	}
	return SmartCardInfo{Available: smartcard.Available, Found: smartcard.DefaultLibraries(), Reports: a.cards.List(use), LoggedIn: a.cards.LoggedIn()}
}

// ForgetSmartCardPins logs out of every card: the PINs are asked again.
func (a *App) ForgetSmartCardPins() { a.cards.Forget() }
