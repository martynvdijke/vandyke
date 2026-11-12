import 'package:flutter_test/flutter_test.dart';
import 'package:vandyke/src/deep_links.dart';
import 'package:vandyke/src/notifications/push_config.dart';
import 'package:vandyke/src/notifications/push_service.dart';

void main() {
  group('PushConfig', () {
    Map<String, dynamic> payload({
      bool enabled = true,
      bool android = true,
      Map<String, dynamic>? firebase,
    }) {
      return {
        'enabled': enabled,
        'realtime': true,
        'web_push': true,
        'android': android,
        'vapid_public_key': 'vapid',
        'firebase':
            firebase ??
            {
              'api_key': 'api',
              'app_id': 'app',
              'messaging_sender_id': 'sender',
              'project_id': 'project',
            },
      };
    }

    test('a complete answer is usable', () {
      final config = PushConfig.fromJson(payload());
      expect(config.usable, isTrue);
      expect(config.realtime, isTrue);
      expect(config.webPush, isTrue);
      expect(config.android, isTrue);
      expect(config.vapidPublicKey, 'vapid');
      expect(config.apiKey, 'api');
      expect(config.projectId, 'project');
    });

    test('a server without push is not usable', () {
      expect(PushConfig.fromJson(payload(enabled: false)).usable, isFalse);
    });

    test('android switched off is not usable', () {
      expect(PushConfig.fromJson(payload(android: false)).usable, isFalse);
    });

    test('a missing firebase field is not usable', () {
      final config = PushConfig.fromJson(
        payload(
          firebase: {
            'api_key': 'api',
            'app_id': 'app',
            'messaging_sender_id': 'sender',
          },
        ),
      );
      expect(config.usable, isFalse);
    });

    test('an answer without the android flag stays usable', () {
      final json = payload()..remove('android');
      expect(PushConfig.fromJson(json).usable, isTrue);
    });
  });

  group('notificationEntryUri', () {
    test('keeps an absolute entry url', () {
      final uri = notificationEntryUri({
        'url': 'https://ledger.example/e/42',
      }, 'https://ledger.example');
      expect(uri, isNotNull);
      expect(parseEntryLink(uri!)?.id, 42);
    });

    test('builds a permalink from a bare entry id', () {
      final uri = notificationEntryUri({
        'entry_id': '7',
      }, 'https://ledger.example');
      expect(uri?.toString(), entryPermalink('https://ledger.example', 7));
      expect(parseEntryLink(uri!)?.id, 7);
    });

    test('prefers the url over the id', () {
      final uri = notificationEntryUri({
        'url': 'https://other.example/e/3',
        'entry_id': '9',
      }, 'https://ledger.example');
      expect(parseEntryLink(uri!)?.id, 3);
    });

    test('falls back to the id when the url is not an entry link', () {
      final uri = notificationEntryUri({
        'url': 'https://ledger.example/settings',
        'entry_id': '9',
      }, 'https://ledger.example');
      expect(uri?.toString(), entryPermalink('https://ledger.example', 9));
    });

    test('returns null when the message names no entry', () {
      expect(notificationEntryUri(const {}, 'https://ledger.example'), isNull);
      expect(
        notificationEntryUri({'entry_id': 'nope'}, 'https://ledger.example'),
        isNull,
      );
    });
  });
}
