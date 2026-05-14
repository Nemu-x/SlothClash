//go:build darwin && cgo

#import <Cocoa/Cocoa.h>

extern void slothTrayOnShow(void);
extern void slothTrayOnHide(void);
extern void slothTrayOnToggleConnect(void);
extern void slothTrayOnQuit(void);
extern void slothTrayOnReady(void);
extern void slothTrayOnStopped(void);
extern void slothTrayDispatch(int op);

@interface SlothTrayHandler : NSObject
@end

@implementation SlothTrayHandler
- (void)onShow:(id)sender { (void)sender; slothTrayOnShow(); }
- (void)onHide:(id)sender { (void)sender; slothTrayOnHide(); }
- (void)onToggleConnect:(id)sender { (void)sender; slothTrayOnToggleConnect(); }
- (void)onQuit:(id)sender { (void)sender; slothTrayOnQuit(); }
- (void)onNavSettings:(id)sender { (void)sender; slothTrayDispatch(6); }
@end

static NSData *gMonoPNG = nil;
static NSStatusItem *gStatusItem = nil;
static SlothTrayHandler *gHandler = nil;
static NSMenu *gMenu = nil;
// Live reference to the Connect/Disconnect item so the Go-side poller can
// rewrite its title as connection state changes.
static NSMenuItem *gToggleItem = nil;
static BOOL gTrayWanted = NO;
static NSUInteger gTrayGeneration = 0;

// Localized labels populated by SlothTrayConfigureLabels before SlothTrayStart.
// nil means "fall back to English" (first-launch path on macOS before the
// frontend has had a chance to call SetUiLanguage).
static NSString *gLabelShowWindow = nil;
static NSString *gLabelSettings = nil;
static NSString *gLabelQuit = nil;
static NSString *gLabelConnect = nil;

static NSString *SlothCopyCString(const char *s, NSString *fallback) {
    if (s == NULL) return [fallback retain];
    NSString *out = [NSString stringWithUTF8String:s];
    if (out == nil || out.length == 0) return [fallback retain];
    return [out retain];
}

void SlothTrayConfigureLabels(const char *showWindow,
                              const char *settings,
                              const char *quit,
                              const char *connect) {
    if (gLabelShowWindow != nil) { [gLabelShowWindow release]; gLabelShowWindow = nil; }
    if (gLabelSettings != nil) { [gLabelSettings release]; gLabelSettings = nil; }
    if (gLabelQuit != nil) { [gLabelQuit release]; gLabelQuit = nil; }
    if (gLabelConnect != nil) { [gLabelConnect release]; gLabelConnect = nil; }
    gLabelShowWindow = SlothCopyCString(showWindow, @"Show Window");
    gLabelSettings = SlothCopyCString(settings, @"Settings…");
    gLabelQuit = SlothCopyCString(quit, @"Quit Sloth Clash");
    gLabelConnect = SlothCopyCString(connect, @"Connect");
}

void SlothTrayRegisterMonoPNG(const unsigned char *bytes, int length) {
    if (gMonoPNG != nil) {
        [gMonoPNG release];
        gMonoPNG = nil;
    }
    if (bytes == NULL || length <= 0) {
        return;
    }
    gMonoPNG = [[NSData dataWithBytes:bytes length:(NSUInteger)length] retain];
}

/// Prefer compile-embedded mono.png; then bundle trayicons (Wails); then app icon / SF Symbols.
static NSImage *SlothTrayTemplateImage(void) {
    if (gMonoPNG != nil && [gMonoPNG length] > 0) {
        NSImage *img = [[NSImage alloc] initWithData:gMonoPNG];
        if (img != nil && img.valid) {
            [img setTemplate:YES];
            return img;
        }
    }

    NSBundle *bundle = [NSBundle mainBundle];
    NSString *slothPng = [bundle pathForResource:@"sloth" ofType:@"png" inDirectory:@"trayicons"];
    if (slothPng != nil) {
        NSImage *img = [[NSImage alloc] initWithContentsOfFile:slothPng];
        if (img != nil && img.valid) {
            [img setTemplate:YES];
            return img;
        }
    }

    NSString *icns = [bundle pathForResource:@"iconfile" ofType:@"icns"];
    if (icns != nil) {
        NSImage *appIcon = [[NSImage alloc] initWithContentsOfFile:icns];
        if (appIcon != nil && appIcon.valid) {
            [appIcon setTemplate:YES];
            return appIcon;
        }
    }

    NSImage *icon = nil;
    if (@available(macOS 11.0, *)) {
        icon = [NSImage imageWithSystemSymbolName:@"pawprint"
                         accessibilityDescription:@"Sloth Clash"];
        if (icon != nil) {
            NSImageSymbolConfiguration *cfg =
                [NSImageSymbolConfiguration configurationWithPointSize:14
                                                                weight:NSFontWeightRegular];
            icon = [icon imageWithSymbolConfiguration:cfg];
            if (icon != nil) {
                [icon setTemplate:YES];
                return icon;
            }
        }
    }
    if (@available(macOS 11.0, *)) {
        icon = [NSImage imageWithSystemSymbolName:@"network"
                         accessibilityDescription:@"Sloth Clash"];
        if (icon != nil) {
            NSImageSymbolConfiguration *cfg =
                [NSImageSymbolConfiguration configurationWithPointSize:14
                                                                weight:NSFontWeightRegular];
            icon = [icon imageWithSymbolConfiguration:cfg];
            if (icon != nil) {
                [icon setTemplate:YES];
                return icon;
            }
        }
    }

    NSString *png = [bundle pathForResource:@"appicon" ofType:@"png"];
    if (png != nil) {
        icon = [[NSImage alloc] initWithContentsOfFile:png];
        if (icon != nil && icon.valid) {
            [icon setTemplate:YES];
            return icon;
        }
    }
    return nil;
}

/// Scale the longest side to match the standard menu bar icon range (18-22pt).
/// scripts/optimize-tray-mono.mjs guarantees mono.png ships pre-trimmed at 44px max
/// (= 22pt @2x), so we no longer have to compensate for transparent margins here.
static NSSize SlothTrayNormalisedIconSize(NSImage *icon) {
    NSStatusBar *bar = [NSStatusBar systemStatusBar];
    CGFloat target = (bar != nil && bar.thickness > 1.0) ? (bar.thickness - 2.0) : 22.0;
    if (target < 18.0) target = 18.0;
    if (target > 22.0) target = 22.0;

    NSSize s = icon.size;
    if (s.width < 1.0 || s.height < 1.0) {
        return NSMakeSize(target, target);
    }
    CGFloat m = MAX(s.width, s.height);
    CGFloat scale = target / m;
    return NSMakeSize(round(s.width * scale), round(s.height * scale));
}

static void SlothTrayCreateOnMain(NSUInteger generation) {
    @autoreleasepool {
        if (!gTrayWanted || generation != gTrayGeneration) return;
        if (gStatusItem != nil) return;
        gHandler = [[SlothTrayHandler new] retain];
        // Square length keeps the slot tight; the normalized template icon already
        // fits the standard menu-bar metric so we no longer need NSVariableStatusItemLength.
        gStatusItem = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength] retain];

        NSImage *icon = SlothTrayTemplateImage();
        if (icon != nil) {
            NSSize s = SlothTrayNormalisedIconSize(icon);
            [icon setSize:s];
            [icon setTemplate:YES];
            gStatusItem.button.image = icon;
            gStatusItem.button.imagePosition = NSImageOnly;
            gStatusItem.button.title = @"";
        } else {
            gStatusItem.button.image = nil;
            gStatusItem.button.imagePosition = NSNoImage;
            gStatusItem.button.title = @"SC";
            gStatusItem.button.font = [NSFont menuBarFontOfSize:11];
        }
        gStatusItem.button.toolTip = @"Sloth Clash";
        gStatusItem.button.appearsDisabled = NO;
        gStatusItem.button.hidden = NO;

        gMenu = [[[NSMenu alloc] initWithTitle:@"Sloth Clash"] retain];

        // Minimal menu — every navigation/mode/traffic toggle has a dedicated
        // place in the main window already. Earlier 14-item variant turned
        // the popover into a wall of text and the user-facing message of
        // "Toggle Connect" stayed wrong across state changes because nothing
        // updated the menu item title (the Go-side poller now does, below).
        NSString *showLabel = gLabelShowWindow != nil ? gLabelShowWindow : @"Show Window";
        NSString *connectLabel = gLabelConnect != nil ? gLabelConnect : @"Connect";
        NSString *settingsLabel = gLabelSettings != nil ? gLabelSettings : @"Settings…";
        NSString *quitLabel = gLabelQuit != nil ? gLabelQuit : @"Quit Sloth Clash";

        NSMenuItem *showItem = [[NSMenuItem alloc] initWithTitle:showLabel action:@selector(onShow:) keyEquivalent:@""];
        [showItem setTarget:gHandler];
        [gMenu addItem:showItem];

        gToggleItem = [[NSMenuItem alloc] initWithTitle:connectLabel action:@selector(onToggleConnect:) keyEquivalent:@""];
        [gToggleItem setTarget:gHandler];
        [gMenu addItem:gToggleItem];

        [gMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *settingsItem = [[NSMenuItem alloc] initWithTitle:settingsLabel action:@selector(onNavSettings:) keyEquivalent:@""];
        [settingsItem setTarget:gHandler];
        [gMenu addItem:settingsItem];

        [gMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:quitLabel action:@selector(onQuit:) keyEquivalent:@""];
        [quitItem setTarget:gHandler];
        [gMenu addItem:quitItem];

        gStatusItem.menu = gMenu;
        gStatusItem.visible = YES;
        slothTrayOnReady();
    }
}

// SlothTraySetConnectTitle is called from the Go-side poller every time the
// app's connection status string changes. Marshals the update onto the main
// queue (NSMenuItem setTitle: is main-thread-only) and is a no-op if the
// menu has been torn down. Safe to call after SlothTrayStop.
void SlothTraySetConnectTitle(const char *title) {
    if (title == NULL) return;
    NSString *t = [NSString stringWithUTF8String:title];
    if (t == nil) return;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gToggleItem == nil) return;
        [gToggleItem setTitle:t];
    });
}

void SlothTrayStart(void) {
    gTrayWanted = YES;
    gTrayGeneration++;
    NSUInteger generation = gTrayGeneration;
    dispatch_time_t when = dispatch_time(DISPATCH_TIME_NOW, (int64_t)(120 * NSEC_PER_MSEC));
    dispatch_after(when, dispatch_get_main_queue(), ^{
        SlothTrayCreateOnMain(generation);
    });
}

void SlothTrayStop(void) {
    gTrayWanted = NO;
    gTrayGeneration++;
    void (^cleanup)(void) = ^{
        if (gStatusItem != nil) {
            [[NSStatusBar systemStatusBar] removeStatusItem:gStatusItem];
            [gStatusItem release];
            gStatusItem = nil;
        }
        [gMenu release];
        gMenu = nil;
        [gToggleItem release];
        gToggleItem = nil;
        [gHandler release];
        gHandler = nil;
        if (gMonoPNG != nil) {
            [gMonoPNG release];
            gMonoPNG = nil;
        }
        slothTrayOnStopped();
    };
    if ([NSThread isMainThread]) {
        cleanup();
    } else {
        dispatch_async(dispatch_get_main_queue(), cleanup);
    }
}
