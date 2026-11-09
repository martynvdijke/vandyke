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

  test('PersonCount parses a name and its tally', () {
    final person = PersonCount.fromJson({
      'person': '  Reception ',
      'count': 12,
    });
    expect(person.person, 'Reception');
    expect(person.count, 12);

    final bare = PersonCount.fromJson({'person': 'Concierge'});
    expect(bare.count, 0);
  });

  test('parsePersons reads the items envelope, sorts, and merges names', () {
    final persons = parsePersons({
      'items': [
        {'person': 'zoe', 'count': 2},
        {'person': 'Ana', 'count': 1},
        {'person': 'ZOE', 'count': 3},
        {'person': '   ', 'count': 9},
        {'count': 4},
      ],
      'total': 3,
    });

    expect(persons.map((person) => person.person), ['Ana', 'zoe']);
    expect(persons.first.count, 1);
    expect(persons.last.count, 5);
  });

  test('parsePersons tolerates a missing payload', () {
    expect(parsePersons(null), isEmpty);
    expect(parsePersons({}), isEmpty);
    expect(parsePersons({'items': 'nope'}), isEmpty);
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

  test('YearCount parses a year and its tally', () {
    final year = YearCount.fromJson({'year': '2026', 'count': 40});
    expect(year.year, 2026);
    expect(year.count, 40);
    expect(YearCount.fromJson({'year': 2019}).count, 0);
  });

  test('LedgerStats parses the optional breakdowns', () {
    final stats = LedgerStats.fromJson({
      'total_entries': 214,
      'unique_words': 63,
      'entries_last_30_days': 9,
      'with_photo': 42,
      'by_year': [
        {'year': 2026, 'count': 40},
        {'year': 2019, 'count': 3},
        {'year': 0, 'count': 99},
      ],
      'top_persons': [
        {'person': 'Reception', 'count': 84},
        {'person': 'Concierge', 'count': 31},
        {'person': 'reception', 'count': 84},
        {'person': '   ', 'count': 5},
      ],
      'first_entry_at': '2019-02-03T00:00:00Z',
      'last_entry_at': '2026-11-07T12:00:00Z',
    });

    expect(stats.withPhoto, 42);
    expect(stats.byYear.map((year) => year.year), [2019, 2026]);
    expect(stats.byYear.first.count, 3);
    expect(stats.topPersons.map((person) => person.person), [
      'Reception',
      'Concierge',
    ]);
    expect(stats.topPersons.first.count, 84);
    expect(stats.hasPhotoShare, isTrue);
    expect(stats.photoShare, closeTo(42 / 214, 0.0001));
    expect(stats.photoShareLabel, '20%');
    expect(stats.runningLabel, '7 years, 9 months');
  });

  test('LedgerStats degrades when the breakdowns are missing', () {
    final stats = LedgerStats.fromJson({
      'total_entries': 5,
      'unique_words': 4,
      'entries_last_30_days': 1,
    });

    expect(stats.withPhoto, isNull);
    expect(stats.byYear, isEmpty);
    expect(stats.topPersons, isEmpty);
    expect(stats.hasPhotoShare, isFalse);
    expect(stats.photoShare, 0);
    expect(stats.photoShareLabel, '0%');
    expect(stats.runningLabel, '—');
  });

  test('LedgerStats caps and orders top authors by tally', () {
    final stats = LedgerStats.fromJson({
      'total_entries': 300,
      'unique_words': 40,
      'entries_last_30_days': 2,
      'top_persons': [
        for (var i = 0; i < 12; i++) {'person': 'Person $i', 'count': i + 1},
      ],
    });

    expect(stats.topPersons.length, 8);
    expect(stats.topPersons.first.person, 'Person 11');
    expect(stats.topPersons.first.count, 12);
    expect(stats.topPersons.last.count, 5);
  });

  test('LedgerStats clamps a photo count larger than the ledger', () {
    final stats = LedgerStats.fromJson({
      'total_entries': 5,
      'unique_words': 4,
      'entries_last_30_days': 1,
      'with_photo': 9,
    });

    expect(stats.photoShare, 1);
    expect(stats.photoShareLabel, '100%');
  });

  test('LedgerStats handles a single-day ledger', () {
    final stats = LedgerStats.fromJson({
      'total_entries': 1,
      'unique_words': 1,
      'entries_last_30_days': 1,
      'first_entry_at': '2026-11-07T08:00:00Z',
      'last_entry_at': '2026-11-07T09:00:00Z',
    });

    expect(stats.runningLabel, 'a single day');
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
