import 'package:flutter_test/flutter_test.dart';
import 'package:vandyke/src/api/models.dart';

void main() {
  test('Entry parses snake_case payload and localizes the date', () {
    final entry = Entry.fromJson({
      'id': 12,
      'word': 'van Dike',
      'meaning': 'Should read vanDyke',
      'person': 'Front desk',
      'photo_url': '/api/v1/entries/12/photo',
      'created_at': '2026-03-14T09:30:00Z',
    });

    expect(entry.id, 12);
    expect(entry.word, 'van Dike');
    expect(entry.meaning, 'Should read vanDyke');
    expect(entry.person, 'Front desk');
    expect(entry.hasPhoto, isTrue);
    expect(
      entry.photoFor('https://vandyke.example.com', size: 'display'),
      'https://vandyke.example.com/api/v1/entries/12/photo?size=display',
    );
    expect(entry.createdAt.toUtc(), DateTime.utc(2026, 3, 14, 9, 30));
  });

  test('Entry tolerates missing optional fields', () {
    final entry = Entry.fromJson({
      'id': '3',
      'word': 'Vandyke',
      'meaning': 'Should read vanDyke',
    });

    expect(entry.id, 3);
    expect(entry.person, isEmpty);
    expect(entry.hasPhoto, isFalse);
    expect(entry.photoFor('https://vandyke.example.com'), isEmpty);
  });

  test('EntriesPage parses totals and pagination flags', () {
    final page = EntriesPage.fromJson({
      'items': [
        {'id': 1, 'word': 'A', 'meaning': 'B'},
        {'id': 2, 'word': 'C', 'meaning': 'D'},
      ],
      'total': 60,
      'page': 2,
      'per_page': 25,
      'pages': 3,
    });

    expect(page.items.length, 2);
    expect(page.total, 60);
    expect(page.page, 2);
    expect(page.perPage, 25);
    expect(page.pages, 3);
    expect(page.hasMore, isTrue);
  });

  test('EntriesPage clamps an empty payload to an empty first page', () {
    final page = EntriesPage.fromJson({});
    expect(page.items, isEmpty);
    expect(page.total, 0);
    expect(page.page, 1);
    expect(page.hasMore, isFalse);
  });

  test('LedgerStats parses aggregate counters and dates', () {
    final stats = LedgerStats.fromJson({
      'total_entries': 123,
      'unique_words': 87,
      'entries_last_30_days': 9,
      'first_entry_at': '2019-01-02T03:04:05Z',
      'last_entry_at': '2026-11-07T12:00:00Z',
    });

    expect(stats.total, 123);
    expect(stats.uniqueWords, 87);
    expect(stats.entriesLast30Days, 9);
    expect(stats.firstEntryAt?.toUtc(), DateTime.utc(2019, 1, 2, 3, 4, 5));
    expect(stats.lastEntryAt?.toUtc(), DateTime.utc(2026, 11, 7, 12));
    expect(stats.firstEntryLabel, isNotEmpty);
  });

  test('LedgerStats handles a ledger with no entries', () {
    final stats = LedgerStats.fromJson({
      'total_entries': 0,
      'unique_words': 0,
      'entries_last_30_days': 0,
      'first_entry_at': null,
      'last_entry_at': null,
    });

    expect(stats.total, 0);
    expect(stats.firstEntryAt, isNull);
    expect(stats.lastEntryLabel, '—');
  });
}
