//go:build darwin && cgo

// Package macwin reads where macOS draws the window buttons (close,
// minimise, zoom), so a title bar drawn by the app lines up with them.
package macwin

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// tw_window_buttons writes the centre of the window buttons, measured from
// the top of the window, and the right edge of the zoom button. Returns 0
// when there is no window yet.
static int tw_window_buttons(double *centerY, double *right) {
	__block int ok = 0;
	void (^fetch)(void) = ^{
		NSWindow *w = [NSApp mainWindow];
		if (w == nil) w = [NSApp keyWindow];
		if (w == nil && [[NSApp windows] count] > 0) w = [[NSApp windows] objectAtIndex:0];
		if (w == nil) return;
		NSButton *close = [w standardWindowButton:NSWindowCloseButton];
		NSButton *zoom = [w standardWindowButton:NSWindowZoomButton];
		if (close == nil || zoom == nil || [close superview] == nil) return;
		NSView *frameView = [[w contentView] superview];
		if (frameView == nil) return;
		NSRect c = [close convertRect:[close bounds] toView:frameView];
		NSRect z = [zoom convertRect:[zoom bounds] toView:frameView];
		CGFloat h = [frameView bounds].size.height;
		*centerY = [frameView isFlipped] ? NSMidY(c) : h - NSMidY(c);
		*right = NSMaxX(z);
		ok = 1;
	};
	if ([NSThread isMainThread]) fetch(); else dispatch_sync(dispatch_get_main_queue(), fetch);
	return ok;
}
*/
import "C"

// WindowButtons returns the vertical centre of the window buttons (from the
// top of the window) and the right edge of the last one, in points.
func WindowButtons() (centerY, right float64, ok bool) {
	var cy, r C.double
	if C.tw_window_buttons(&cy, &r) == 0 {
		return 0, 0, false
	}
	return float64(cy), float64(r), true
}
