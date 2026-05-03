#import <Cocoa/Cocoa.h>
#import <objc/message.h>
#import <objc/runtime.h>

extern void slothOnTerminateRequest(void);
extern void slothOnOpenURL(const char *rawURL);

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

static NSApplicationTerminateReply sloth_applicationShouldTerminate(id self, SEL _cmd, NSApplication *sender) {
	(void)self;
	(void)_cmd;
	(void)sender;
	// Mark explicit app quit from Dock/Cmd+Q so OnBeforeClose won't reroute into tray hide.
	slothOnTerminateRequest();
	return NSTerminateNow;
}

static void sloth_applicationOpenURLs(id self, SEL _cmd, NSApplication *app, NSArray<NSURL *> *urls) {
	(void)self;
	(void)_cmd;
	(void)app;
	if (urls == nil || [urls count] == 0) {
		return;
	}
	for (NSURL *u in urls) {
		if (u == nil) {
			continue;
		}
		NSString *s = [u absoluteString];
		if (s == nil || [s length] == 0) {
			continue;
		}
		slothOnOpenURL([s UTF8String]);
	}
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

	SEL terminateSel = @selector(applicationShouldTerminate:);
	struct objc_method_description termDesc =
	    protocol_getMethodDescription(@protocol(NSApplicationDelegate), terminateSel, NO, YES);
	if (termDesc.types == NULL) {
		return;
	}
	if (!class_addMethod(c, terminateSel, (IMP)sloth_applicationShouldTerminate, termDesc.types)) {
		class_replaceMethod(c, terminateSel, (IMP)sloth_applicationShouldTerminate, termDesc.types);
	}

	SEL openURLsSel = @selector(application:openURLs:);
	struct objc_method_description openURLsDesc =
	    protocol_getMethodDescription(@protocol(NSApplicationDelegate), openURLsSel, NO, YES);
	if (openURLsDesc.types != NULL) {
		if (!class_addMethod(c, openURLsSel, (IMP)sloth_applicationOpenURLs, openURLsDesc.types)) {
			class_replaceMethod(c, openURLsSel, (IMP)sloth_applicationOpenURLs, openURLsDesc.types);
		}
	}
}
