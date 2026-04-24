//go:build darwin && cgo && !slothtray

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

- (void)onNavHome:(id)sender { (void)sender; slothTrayDispatch(1); }
- (void)onNavProfiles:(id)sender { (void)sender; slothTrayDispatch(2); }
- (void)onNavProxies:(id)sender { (void)sender; slothTrayDispatch(3); }
- (void)onNavRules:(id)sender { (void)sender; slothTrayDispatch(4); }
- (void)onNavAdvanced:(id)sender { (void)sender; slothTrayDispatch(5); }
- (void)onNavSettings:(id)sender { (void)sender; slothTrayDispatch(6); }

- (void)onModeRule:(id)sender { (void)sender; slothTrayDispatch(10); }
- (void)onModeGlobal:(id)sender { (void)sender; slothTrayDispatch(11); }
- (void)onModeDirect:(id)sender { (void)sender; slothTrayDispatch(12); }

- (void)onTrafficProxy:(id)sender { (void)sender; slothTrayDispatch(20); }
- (void)onTrafficTun:(id)sender { (void)sender; slothTrayDispatch(21); }
@end

static NSData *gMonoPNG = nil;
static NSStatusItem *gStatusItem = nil;
static SlothTrayHandler *gHandler = nil;
static NSMenu *gMenu = nil;
static BOOL gTrayWanted = NO;
static NSUInteger gTrayGeneration = 0;

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

/// Embedded mono.png often has generous transparent margins; scale its max side larger so the
/// subject reads closer to other menu bar icons. Other sources stay closer to system norms.
static NSSize SlothTrayNormalisedIconSize(NSImage *icon, BOOL embeddedMono) {
    NSStatusBar *bar = [NSStatusBar systemStatusBar];
    CGFloat target = (bar != nil && bar.thickness > 1.0) ? (bar.thickness - 2.0) : 22.0;

    if (embeddedMono) {
        if (target < 30.0) {
            target = 30.0;
        }
        if (target > 34.0) {
            target = 34.0;
        }
    } else {
        if (target < 18.0) {
            target = 18.0;
        }
        if (target > 24.0) {
            target = 24.0;
        }
    }

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
        BOOL useEmbeddedMono = (gMonoPNG != nil && [gMonoPNG length] > 0);
        // Variable length gives room for a larger template icon (mono art with padding).
        gStatusItem = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength] retain];

        NSImage *icon = SlothTrayTemplateImage();
        if (icon != nil) {
            NSSize s = SlothTrayNormalisedIconSize(icon, useEmbeddedMono);
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

        NSMenuItem *showItem = [[NSMenuItem alloc] initWithTitle:@"Show Window" action:@selector(onShow:) keyEquivalent:@""];
        [showItem setTarget:gHandler];
        [gMenu addItem:showItem];

        NSMenuItem *hideItem = [[NSMenuItem alloc] initWithTitle:@"Hide Window" action:@selector(onHide:) keyEquivalent:@""];
        [hideItem setTarget:gHandler];
        [gMenu addItem:hideItem];

        [gMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *homeItem = [[NSMenuItem alloc] initWithTitle:@"Open · Home" action:@selector(onNavHome:) keyEquivalent:@""];
        [homeItem setTarget:gHandler];
        [gMenu addItem:homeItem];

        NSMenuItem *profilesItem = [[NSMenuItem alloc] initWithTitle:@"Open · Profiles" action:@selector(onNavProfiles:) keyEquivalent:@""];
        [profilesItem setTarget:gHandler];
        [gMenu addItem:profilesItem];

        NSMenuItem *proxiesItem = [[NSMenuItem alloc] initWithTitle:@"Open · Proxies" action:@selector(onNavProxies:) keyEquivalent:@""];
        [proxiesItem setTarget:gHandler];
        [gMenu addItem:proxiesItem];

        NSMenuItem *rulesItem = [[NSMenuItem alloc] initWithTitle:@"Open · Rules" action:@selector(onNavRules:) keyEquivalent:@""];
        [rulesItem setTarget:gHandler];
        [gMenu addItem:rulesItem];

        NSMenuItem *advItem = [[NSMenuItem alloc] initWithTitle:@"Open · Advanced" action:@selector(onNavAdvanced:) keyEquivalent:@""];
        [advItem setTarget:gHandler];
        [gMenu addItem:advItem];

        NSMenuItem *settingsItem = [[NSMenuItem alloc] initWithTitle:@"Open · Settings" action:@selector(onNavSettings:) keyEquivalent:@""];
        [settingsItem setTarget:gHandler];
        [gMenu addItem:settingsItem];

        [gMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *modeRule = [[NSMenuItem alloc] initWithTitle:@"Mode · Rule" action:@selector(onModeRule:) keyEquivalent:@""];
        [modeRule setTarget:gHandler];
        [gMenu addItem:modeRule];

        NSMenuItem *modeGlobal = [[NSMenuItem alloc] initWithTitle:@"Mode · Global" action:@selector(onModeGlobal:) keyEquivalent:@""];
        [modeGlobal setTarget:gHandler];
        [gMenu addItem:modeGlobal];

        NSMenuItem *modeDirect = [[NSMenuItem alloc] initWithTitle:@"Mode · Direct" action:@selector(onModeDirect:) keyEquivalent:@""];
        [modeDirect setTarget:gHandler];
        [gMenu addItem:modeDirect];

        [gMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *trafficProxy = [[NSMenuItem alloc] initWithTitle:@"Traffic · System proxy" action:@selector(onTrafficProxy:) keyEquivalent:@""];
        [trafficProxy setTarget:gHandler];
        [gMenu addItem:trafficProxy];

        NSMenuItem *trafficTun = [[NSMenuItem alloc] initWithTitle:@"Traffic · TUN" action:@selector(onTrafficTun:) keyEquivalent:@""];
        [trafficTun setTarget:gHandler];
        [gMenu addItem:trafficTun];

        [gMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *toggleItem = [[NSMenuItem alloc] initWithTitle:@"Toggle Connect" action:@selector(onToggleConnect:) keyEquivalent:@""];
        [toggleItem setTarget:gHandler];
        [gMenu addItem:toggleItem];

        [gMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"Quit Sloth Clash" action:@selector(onQuit:) keyEquivalent:@""];
        [quitItem setTarget:gHandler];
        [gMenu addItem:quitItem];

        gStatusItem.menu = gMenu;
        gStatusItem.visible = YES;
        slothTrayOnReady();
    }
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
