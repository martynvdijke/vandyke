import 'dart:async';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/api_client.dart';
import '../api/api_exception.dart';
import '../deep_links.dart';
import 'push_config.dart';

/// Shared-preferences key for the user's notification choice.
const String notificationsEnabledKey = 'notifications.enabled';

/// The app's version, passed by the Flutter tool from pubspec.yaml.
const String _appVersion = String.fromEnvironment(
  'FLUTTER_BUILD_NAME',
  defaultValue: 'unknown',
);

/// Registered before `runApp`. It does nothing but log: a notification-payload
/// message is drawn by the system tray, and data messages are handled when the
/// app opens.
@pragma('vm:entry-point')
Future<void> firebaseMessagingBackgroundHandler(RemoteMessage message) async {
  debugPrint('Push message received in the background: ${message.messageId}');
}

/// Reads the entry a notification points at.
///
/// The backend sends either an absolute `url` (for example
/// `https://host/e/42`) or a bare `entry_id`; the id is turned into the same
/// permalink the app already understands, so one deep-link router handles both.
Uri? notificationEntryUri(Map<String, dynamic> data, String serverUrl) {
  final raw = (data['url'] ?? '').toString().trim();
  if (raw.isNotEmpty) {
    final uri = Uri.tryParse(raw);
    if (uri != null && parseEntryLink(uri) != null) return uri;
  }
  final id = int.tryParse((data['entry_id'] ?? '').toString().trim());
  if (id != null && id > 0) {
    return Uri.parse(entryPermalink(serverUrl, id));
  }
  return null;
}

/// Starts Firebase Cloud Messaging from configuration fetched at runtime, so
/// no google-services.json is needed in the build.
class PushService {
  PushService(this._api);

  final ApiClient _api;

  final _foreground = StreamController<RemoteMessage>.broadcast();
  final _opened = StreamController<RemoteMessage>.broadcast();

  StreamSubscription<RemoteMessage>? _foregroundSub;
  StreamSubscription<RemoteMessage>? _openedSub;
  StreamSubscription<String>? _tokenRefreshSub;
  Future<bool>? _starting;

  /// Messages that arrive while the app is in the foreground.
  Stream<RemoteMessage> get foregroundMessages => _foreground.stream;

  /// Messages whose notification was tapped while the app was running.
  Stream<RemoteMessage> get openedMessages => _opened.stream;

  String get platform => switch (defaultTargetPlatform) {
    TargetPlatform.android => 'android',
    TargetPlatform.iOS => 'ios',
    _ => defaultTargetPlatform.name.toLowerCase(),
  };

  /// A label the server can show in its device list.
  String get deviceName => switch (defaultTargetPlatform) {
    TargetPlatform.android => 'Android device',
    TargetPlatform.iOS => 'iPhone',
    _ => defaultTargetPlatform.name,
  };

  /// Reads `/push/config`; a missing or unreachable endpoint means push is not
  /// offered on this server, which is never an error the UI has to show.
  Future<PushConfig?> fetchConfig() async {
    try {
      return await _api.pushConfig();
    } catch (error) {
      debugPrint('Push configuration could not be read: $error');
      return null;
    }
  }

  /// Restores Firebase for a device that had notifications switched on, so a
  /// tap that launched the app and token rotation keep working after a restart.
  Future<bool> prepare() async {
    if (Firebase.apps.isNotEmpty) {
      _listen();
      return true;
    }
    if (!await _readEnabled()) return false;
    final config = await fetchConfig();
    if (config == null || !config.usable) return false;
    return _initialize(config);
  }

  /// Turns notifications on: starts Firebase, asks for permission, registers
  /// this device's token and remembers the choice. Returns false when the
  /// server has no usable configuration or the token cannot be obtained.
  Future<bool> enable() async {
    final config = await fetchConfig();
    if (config == null || !config.usable) return false;
    if (!await _initialize(config)) return false;

    try {
      await FirebaseMessaging.instance.requestPermission();
    } catch (error) {
      debugPrint('Notification permission could not be requested: $error');
    }

    final token = await _token();
    if (token == null) return false;
    if (!await _register(token)) return false;
    await _writeEnabled(true);
    return true;
  }

  /// Turns notifications off: removes the token from the server and from
  /// Firebase, then remembers the choice.
  Future<void> disable() async {
    final token = await _token();
    if (token != null) {
      try {
        await _api.unregisterDevice(token);
      } catch (error) {
        debugPrint('Push token could not be removed from the server: $error');
      }
    }
    await _tokenRefreshSub?.cancel();
    _tokenRefreshSub = null;
    try {
      if (Firebase.apps.isNotEmpty) {
        // The backend registers FCM registration tokens, which the newer
        // FID-based API does not return.
        // ignore: deprecated_member_use
        await FirebaseMessaging.instance.deleteToken();
      }
    } catch (error) {
      debugPrint('Push token could not be deleted: $error');
    }
    await _writeEnabled(false);
  }

  /// The stored choice, but only true while the server still offers push.
  /// A configuration the caller already fetched can be passed in.
  Future<bool> isEnabled({PushConfig? config}) async {
    if (!await _readEnabled()) return false;
    final resolved = config ?? await fetchConfig();
    return resolved?.usable ?? false;
  }

  /// The notification that launched the app, if it was opened from one.
  Future<RemoteMessage?> initialMessage() async {
    if (Firebase.apps.isEmpty) return null;
    try {
      return await FirebaseMessaging.instance.getInitialMessage();
    } catch (error) {
      debugPrint('Launching notification could not be read: $error');
      return null;
    }
  }

  void dispose() {
    _foregroundSub?.cancel();
    _openedSub?.cancel();
    _tokenRefreshSub?.cancel();
    _foreground.close();
    _opened.close();
  }

  Future<bool> _initialize(PushConfig config) {
    final pending = _starting;
    if (pending != null) return pending;
    final future = _start(config);
    _starting = future;
    return future;
  }

  Future<bool> _start(PushConfig config) async {
    try {
      await Firebase.initializeApp(
        options: FirebaseOptions(
          apiKey: config.apiKey,
          appId: config.appId,
          messagingSenderId: config.messagingSenderId,
          projectId: config.projectId,
        ),
      );
    } on FirebaseException catch (error) {
      // A second call while the first one still runs is harmless.
      if (error.code != 'duplicate-app') {
        debugPrint('Firebase could not start: ${error.message ?? error.code}');
        _starting = null;
        return false;
      }
    } catch (error) {
      debugPrint('Firebase could not start: $error');
      _starting = null;
      return false;
    }

    _listen();
    final token = await _token();
    if (token != null) unawaited(_register(token));
    return true;
  }

  void _listen() {
    _foregroundSub ??= FirebaseMessaging.onMessage.listen(_foreground.add);
    _openedSub ??= FirebaseMessaging.onMessageOpenedApp.listen(_opened.add);
    // ignore: deprecated_member_use
    _tokenRefreshSub ??= FirebaseMessaging.instance.onTokenRefresh.listen(
      (token) => unawaited(_register(token)),
    );
  }

  Future<String?> _token() async {
    if (Firebase.apps.isEmpty) return null;
    try {
      // ignore: deprecated_member_use
      final token = await FirebaseMessaging.instance.getToken();
      final value = token?.trim() ?? '';
      return value.isEmpty ? null : value;
    } catch (error) {
      debugPrint('Push token could not be read: $error');
      return null;
    }
  }

  Future<bool> _register(String token) async {
    try {
      await _api.registerDevice(
        token: token,
        platform: platform,
        deviceName: deviceName,
        appVersion: _appVersion,
      );
      return true;
    } on ApiException catch (error) {
      debugPrint('Push token could not be registered: ${error.message}');
      return false;
    } catch (error) {
      debugPrint('Push token could not be registered: $error');
      return false;
    }
  }

  Future<bool> _readEnabled() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      return prefs.getBool(notificationsEnabledKey) ?? false;
    } catch (_) {
      return false;
    }
  }

  Future<void> _writeEnabled(bool value) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setBool(notificationsEnabledKey, value);
    } catch (error) {
      debugPrint('Notification setting could not be saved: $error');
    }
  }
}
