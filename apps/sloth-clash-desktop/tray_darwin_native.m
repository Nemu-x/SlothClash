#import <Cocoa/Cocoa.h>

extern void slothTrayOnShow(void);
extern void slothTrayOnHide(void);
extern void slothTrayOnToggleConnect(void);
extern void slothTrayOnQuit(void);
extern void slothTrayOnReady(void);
extern void slothTrayOnStopped(void);

@interface SlothTrayHandler : NSObject
@end

@implementation SlothTrayHandler
- (void)onShow:(id)sender { (void)sender; slothTrayOnShow(); }
- (void)onHide:(id)sender { (void)sender; slothTrayOnHide(); }
- (void)onToggleConnect:(id)sender { (void)sender; slothTrayOnToggleConnect(); }
- (void)onQuit:(id)sender { (void)sender; slothTrayOnQuit(); }
@end

static NSStatusItem *gStatusItem = nil;
static SlothTrayHandler *gHandler = nil;
static NSMenu *gMenu = nil;
static BOOL gTrayWanted = NO;
static NSUInteger gTrayGeneration = 0;

static void SlothTrayCreateOnMain(NSUInteger generation) {
    @autoreleasepool {
        if (!gTrayWanted || generation != gTrayGeneration) return;
        if (gStatusItem != nil) return;
        gHandler = [SlothTrayHandler new];
        gStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];

        // Standard macOS menu bar item: monochrome template image, no forced text hacks.
        NSImage *icon = [NSImage imageNamed:NSImageNameActionTemplate];
        if (icon != nil) {
            [icon setTemplate:YES];
            gStatusItem.button.image = icon;
        }
        gStatusItem.button.title = @"";
        gStatusItem.button.imagePosition = NSImageOnly;
        gStatusItem.button.toolTip = @"Sloth Clash";
        gStatusItem.button.appearsDisabled = NO;
        gStatusItem.button.hidden = NO;

        gMenu = [[NSMenu alloc] initWithTitle:@"Sloth Clash"];
        NSMenuItem *showItem = [[NSMenuItem alloc] initWithTitle:@"Show Window" action:@selector(onShow:) keyEquivalent:@""];
        [showItem setTarget:gHandler];
        [gMenu addItem:showItem];

        NSMenuItem *hideItem = [[NSMenuItem alloc] initWithTitle:@"Hide Window" action:@selector(onHide:) keyEquivalent:@""];
        [hideItem setTarget:gHandler];
        [gMenu addItem:hideItem];

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
    void (^createNow)(void) = ^{
        SlothTrayCreateOnMain(generation);
    };
    if ([NSThread isMainThread]) {
        dispatch_async(dispatch_get_main_queue(), createNow);
    } else {
        dispatch_async(dispatch_get_main_queue(), createNow);
    }
}

void SlothTrayStop(void) {
    gTrayWanted = NO;
    gTrayGeneration++;
    void (^cleanup)(void) = ^{
        if (gStatusItem != nil) {
            [[NSStatusBar systemStatusBar] removeStatusItem:gStatusItem];
            gStatusItem = nil;
        }
        gMenu = nil;
        gHandler = nil;
        slothTrayOnStopped();
    };
    if ([NSThread isMainThread]) {
        cleanup();
    } else {
        dispatch_async(dispatch_get_main_queue(), cleanup);
    }
}
