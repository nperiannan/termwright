package team

import (
	"regexp"
	"testing"
)

// The page's <html> carries data-theme once a theme is chosen: a handler bound to
// every [data-theme] also lands on it, and its redraw on every click undid each
// check box (the box was drawn again before its change event).
func TestAdminThemeHandlerOnButtonsOnly(t *testing.T) {
	if regexp.MustCompile(`on\('\[data-theme\]'`).Match(adminHTML) {
		t.Fatal("the theme handler would also be set on <html>")
	}
}
