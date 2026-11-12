import '../api/models.dart';

/// The server's answer to `GET /api/v1/push/config`.
///
/// The Firebase client configuration is handed over here instead of being
/// baked into the build, so the app ships without a google-services.json.
class PushConfig {
  const PushConfig({
    required this.enabled,
    this.realtime = false,
    this.webPush = false,
    this.android = true,
    this.vapidPublicKey = '',
    this.apiKey = '',
    this.appId = '',
    this.messagingSenderId = '',
    this.projectId = '',
  });

  /// The server's master switch for notifications.
  final bool enabled;

  /// Capabilities of the other channels; only [android] concerns this app.
  final bool realtime;
  final bool webPush;
  final bool android;

  final String vapidPublicKey;

  final String apiKey;
  final String appId;
  final String messagingSenderId;
  final String projectId;

  /// Push can only start when the server has it switched on for Android and
  /// has supplied every field Firebase needs; a half-filled answer is treated
  /// as absent.
  bool get usable =>
      enabled &&
      android &&
      apiKey.isNotEmpty &&
      appId.isNotEmpty &&
      messagingSenderId.isNotEmpty &&
      projectId.isNotEmpty;

  factory PushConfig.fromJson(Map<String, dynamic> json) {
    final firebase = asMap(json['firebase']);
    return PushConfig(
      enabled: boolOf(json, 'enabled'),
      realtime: boolOf(json, 'realtime'),
      webPush: boolOf(json, 'web_push'),
      // Older servers predate the per-channel flags; a missing flag means
      // Android was not switched off.
      android: boolOf(json, 'android', true),
      vapidPublicKey: stringOf(json, 'vapid_public_key').trim(),
      apiKey: stringOf(firebase, 'api_key').trim(),
      appId: stringOf(firebase, 'app_id').trim(),
      messagingSenderId: stringOf(firebase, 'messaging_sender_id').trim(),
      projectId: stringOf(firebase, 'project_id').trim(),
    );
  }
}
