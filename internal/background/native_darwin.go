//go:build darwin && cgo

package background

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework UserNotifications
#include <stdlib.h>
#import <Cocoa/Cocoa.h>
#import <UserNotifications/UserNotifications.h>

extern void mmtClicked(char *channel);
extern void mmtNotifyFailed(char *msg);
extern void mmtPermission(int status);

@interface MMTDelegate : NSObject <NSApplicationDelegate, UNUserNotificationCenterDelegate>
@end

static void report(NSError *err) {
	if (err != nil) {
		mmtNotifyFailed((char *)[err.localizedDescription UTF8String]);
	}
}

@implementation MMTDelegate
// Notifications can only be set up once the app has checked in with the
// system. macOS only shows the permission prompt to an app in the
// foreground, so the first time the agent briefly becomes a regular app,
// asks, and goes back to running unseen.
- (void)applicationDidFinishLaunching:(NSNotification *)note {
	UNUserNotificationCenter *c = [UNUserNotificationCenter currentNotificationCenter];
	c.delegate = self;
	[c getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *s) {
		mmtPermission((int)s.authorizationStatus);
		if (s.authorizationStatus != UNAuthorizationStatusNotDetermined) {
			return;
		}
		dispatch_async(dispatch_get_main_queue(), ^{
			[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
			[NSApp activateIgnoringOtherApps:YES];
			[c requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound)
			                 completionHandler:^(BOOL granted, NSError *err) {
				report(err);
				mmtPermission(granted ? (int)UNAuthorizationStatusAuthorized : (int)UNAuthorizationStatusDenied);
				dispatch_async(dispatch_get_main_queue(), ^{
					[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
				});
			}];
		});
	}];
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
    didReceiveNotificationResponse:(UNNotificationResponse *)response
             withCompletionHandler:(void (^)(void))done {
	NSString *ch = response.notification.request.content.userInfo[@"channel"];
	mmtClicked((char *)[(ch ?: @"") UTF8String]);
	done();
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)n
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))done {
	done(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList | UNNotificationPresentationOptionSound);
}
@end

static MMTDelegate *delegate;

// mmtRun runs the Cocoa loop on the main thread. It never returns.
static void mmtRun(void) {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	delegate = [MMTDelegate new];
	NSApp.delegate = delegate;
	[NSApp run];
}

static NSString *str(const char *s) {
	return [NSString stringWithUTF8String:s] ?: @"";
}

static void mmtNotify(const char *title, const char *body, const char *channel) {
	UNMutableNotificationContent *ct = [UNMutableNotificationContent new];
	ct.title = str(title);
	ct.body = str(body);
	ct.sound = [UNNotificationSound defaultSound];
	ct.threadIdentifier = str(channel);
	ct.userInfo = @{@"channel": str(channel)};
	UNNotificationRequest *rq = [UNNotificationRequest requestWithIdentifier:[[NSUUID UUID] UUIDString]
	                                                                 content:ct
	                                                                 trigger:nil];
	[[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:rq
	                                                       withCompletionHandler:^(NSError *err) { report(err); }];
}
*/
import "C"

import "unsafe"

const nativeNotifications = true

// runMain runs the Cocoa event loop; it must be called on the main thread
// and does not return.
func runMain(click func(channel string)) {
	onClick = click
	C.mmtRun()
}

func show(title, body, channel string) {
	t, b, c := C.CString(title), C.CString(body), C.CString(channel)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(b))
	defer C.free(unsafe.Pointer(c))
	C.mmtNotify(t, b, c)
}
