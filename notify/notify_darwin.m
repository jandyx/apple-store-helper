//go:build darwin

#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>

extern void notifyFallback(char *title, char *body);

// 前台时也展示横幅：不设置 delegate 时，App 在最前面时 macOS 不会弹出横幅
@interface ASHNotifyDelegate : NSObject <UNUserNotificationCenterDelegate>
@end

@implementation ASHNotifyDelegate
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
    // Info.plist 最低系统版本为 macOS 11
    completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList | UNNotificationPresentationOptionSound);
}
@end

static ASHNotifyDelegate *notifyDelegate = nil;
static int notifyNum = 0;

static bool isBundled(void) {
    return [[NSBundle mainBundle] bundleIdentifier] != nil;
}

bool notifySetup(void) {
    if (!isBundled()) {
        return false;
    }
    @autoreleasepool {
        UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
        if (notifyDelegate == nil) {
            notifyDelegate = [[ASHNotifyDelegate alloc] init];
        }
        center.delegate = notifyDelegate;
        [center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound)
                              completionHandler:^(BOOL granted, NSError *_Nullable error) {
            if (!granted) {
                NSLog(@"notification permission not granted: %@", error);
            }
        }];
    }
    return true;
}

void notifySend(char *cTitle, char *cBody) {
    @autoreleasepool {
        NSString *title = [NSString stringWithUTF8String:cTitle];
        NSString *body = [NSString stringWithUTF8String:cBody];

        if (!isBundled()) {
            notifyFallback((char *)[title UTF8String], (char *)[body UTF8String]);
            return;
        }

        UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
        [center getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
            if (settings.authorizationStatus != UNAuthorizationStatusAuthorized &&
                settings.authorizationStatus != UNAuthorizationStatusProvisional) {
                notifyFallback((char *)[title UTF8String], (char *)[body UTF8String]);
                return;
            }

            UNMutableNotificationContent *content = [[[UNMutableNotificationContent alloc] init] autorelease];
            content.title = title;
            content.body = body;
            content.sound = [UNNotificationSound defaultSound];

            notifyNum++;
            NSString *identifier = [NSString stringWithFormat:@"ash-notify-%d", notifyNum];
            UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:identifier content:content trigger:nil];
            [center addNotificationRequest:request withCompletionHandler:^(NSError *_Nullable error) {
                if (error != nil) {
                    NSLog(@"send notification failed: %@", error);
                    notifyFallback((char *)[title UTF8String], (char *)[body UTF8String]);
                }
            }];
        }];
    }
}
