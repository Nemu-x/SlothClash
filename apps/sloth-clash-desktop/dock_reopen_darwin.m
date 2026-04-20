#import <Cocoa/Cocoa.h>
#import <objc/message.h>
#import <objc/runtime.h>

// Wails does not implement applicationShouldHandleReopen:hasVisibleWindows:.
// After WindowHide (orderOut), clicking the Dock often does nothing. Install the
// delegate method at runtime so plain `go test` does not link _OBJC_CLASS_$_AppDelegate.

static BOOL sloth_applicationShouldHandleReopen(id self, SEL _cmd, NSApplication *sender, BOOL flag) {
	(void)_cmd;
	(void)sender;
	(void)flag;
	SEL mwSel = sel_registerName("mainWindow");
	id win = ((id (*)(id, SEL))objc_msgSend)(self, mwSel);
	if (win != nil && [win isKindOfClass:[NSWindow class]]) {
		NSWindow *w = (NSWindow *)win;
		if ([w isMiniaturized]) {
			[w deminiaturize:nil];
		}
		[w makeKeyAndOrderFront:nil];
	}
	[NSApp activateIgnoringOtherApps:YES];
	return YES;
}

void sloth_link_dock_reopen(void) {
	Class c = objc_getClass("AppDelegate");
	if (c == NULL) {
		return;
	}
	SEL sel = @selector(applicationShouldHandleReopen:hasVisibleWindows:);
	if (class_getInstanceMethod(c, sel) != NULL) {
		return;
	}
	struct objc_method_description desc =
	    protocol_getMethodDescription(@protocol(NSApplicationDelegate), sel, NO, YES);
	if (desc.types == NULL) {
		return;
	}
	class_addMethod(c, sel, (IMP)sloth_applicationShouldHandleReopen, desc.types);
}
