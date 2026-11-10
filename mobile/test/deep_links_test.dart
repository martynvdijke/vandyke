import 'package:flutter_test/flutter_test.dart';
import 'package:vandyke/src/deep_links.dart';

void main() {
  const server = 'https://vandyke.thor.edu';

  group('parseEntryLink', () {
    test('parses an https permalink', () {
      final link = parseEntryLink(Uri.parse('$server/e/42'));

      expect(link, isNotNull);
      expect(link!.id, 42);
    });

    test('parses the custom scheme', () {
      final link = parseEntryLink(Uri.parse('vandyke://e/7'));

      expect(link, isNotNull);
      expect(link!.id, 7);
    });

    test('ignores a non-numeric id', () {
      expect(parseEntryLink(Uri.parse('$server/e/abc')), isNull);
      expect(parseEntryLink(Uri.parse('vandyke://e/abc')), isNull);
      expect(parseEntryLink(Uri.parse('$server/e/')), isNull);
    });

    test('ignores links that are not entry permalinks', () {
      expect(parseEntryLink(Uri.parse('$server/entries/42')), isNull);
      expect(parseEntryLink(Uri.parse('$server/')), isNull);
      expect(parseEntryLink(Uri.parse('vandyke://stats')), isNull);
    });
  });

  group('linkBelongsToLedger', () {
    test('accepts the configured host', () {
      expect(
        linkBelongsToLedger(Uri.parse('$server/e/42'), server),
        isTrue,
      );
      expect(
        linkBelongsToLedger(Uri.parse('vandyke://e/42'), server),
        isTrue,
      );
    });

    test('rejects a foreign host', () {
      expect(
        linkBelongsToLedger(
          Uri.parse('https://elsewhere.example/e/42'),
          server,
        ),
        isFalse,
      );
    });

    test('rejects a different port on the same host', () {
      expect(
        linkBelongsToLedger(
          Uri.parse('https://vandyke.thor.edu:8443/e/42'),
          server,
        ),
        isFalse,
      );
    });
  });

  test('entryPermalink builds the canonical absolute URL', () {
    expect(entryPermalink(server, 42), '$server/e/42');
    expect(entryPermalink('$server/', 7), '$server/e/7');
    expect(
      entryPermalink('ledger.example.com/api/v1', 3),
      'https://ledger.example.com/e/3',
    );
  });
}
