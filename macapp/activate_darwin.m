#import <AppKit/AppKit.h>

extern void goAppDidBecomeActive(void);

// 点 Dock 图标、再次打开 App、Cmd+Tab 切回来都会触发 didBecomeActive
void observeActivation(void) {
    [[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidBecomeActiveNotification
                                                      object:nil
                                                       queue:[NSOperationQueue mainQueue]
                                                  usingBlock:^(NSNotification *note) {
        goAppDidBecomeActive();
    }];
}
