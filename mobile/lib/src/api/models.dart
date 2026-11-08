import 'package:intl/intl.dart';

/// Ledger payloads are lower_snake_case; parsing stays tolerant of absent and
/// loosely typed values so one odd row cannot break the feed.
Object? _lowerValue(Object? value) {
  if (value is Map) {
    return value.map(
      (key, item) => MapEntry(key.toString().toLowerCase(), _lowerValue(item)),
    );
  }
  if (value is List) return value.map(_lowerValue).toList();
  return value;
}

Map<String, dynamic> lowerKeys(Object? value) {
  if (value is! Map) return const {};
  return value.map(
    (key, item) => MapEntry(key.toString().toLowerCase(), _lowerValue(item)),
  );
}

Map<String, dynamic> asMap(Object? value) {
  if (value is Map) return lowerKeys(value);
  return const {};
}

List<Map<String, dynamic>> asList(Object? value) {
  if (value is List) {
    return value.map(asMap).where((m) => m.isNotEmpty).toList();
  }
  return const [];
}

String stringOf(Map<String, dynamic> json, String key, [String fallback = '']) {
  final value = json[key];
  if (value == null) return fallback;
  return value.toString();
}

int intOf(Map<String, dynamic> json, String key, [int fallback = 0]) {
  final value = json[key];
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '') ?? fallback;
}

bool boolOf(Map<String, dynamic> json, String key, [bool fallback = false]) {
  final value = json[key];
  if (value is bool) return value;
  if (value is num) return value != 0;
  final text = value?.toString().toLowerCase();
  if (text == 'true' || text == '1') return true;
  if (text == 'false' || text == '0') return false;
  return fallback;
}

DateTime? _dateOf(Map<String, dynamic> json, String key) {
  final raw = stringOf(json, key);
  if (raw.isEmpty) return null;
  return DateTime.tryParse(raw)?.toLocal();
}

/// One misspelling in the ledger.
class Entry {
  const Entry({
    required this.id,
    required this.word,
    required this.meaning,
    this.person = '',
    this.photoUrl = '',
    required this.createdAt,
  });

  final int id;
  final String word;
  final String meaning;
  final String person;
  final String photoUrl;
  final DateTime createdAt;

  bool get hasPhoto => photoUrl.isNotEmpty;

  /// `photo_url` is returned as a root-relative API path.
  String photoFor(String baseUrl, {String size = 'thumb'}) {
    if (!hasPhoto) return '';
    final path = photoUrl.startsWith('http') ? photoUrl : '$baseUrl$photoUrl';
    return '$path?size=$size';
  }

  factory Entry.fromJson(Map<String, dynamic> json) {
    final map = lowerKeys(json);
    return Entry(
      id: intOf(map, 'id'),
      word: stringOf(map, 'word'),
      meaning: stringOf(map, 'meaning'),
      person: stringOf(map, 'person'),
      photoUrl: stringOf(map, 'photo_url'),
      createdAt:
          _dateOf(map, 'created_at') ?? DateTime.fromMillisecondsSinceEpoch(0),
    );
  }
}

/// A page of ledger entries as returned by `GET /api/v1/entries`.
class EntriesPage {
  const EntriesPage({
    required this.items,
    required this.total,
    required this.page,
    required this.perPage,
    required this.pages,
  });

  final List<Entry> items;
  final int total;
  final int page;
  final int perPage;
  final int pages;

  bool get hasMore => page < pages;

  static final EntriesPage empty = const EntriesPage(
    items: [],
    total: 0,
    page: 1,
    perPage: 25,
    pages: 0,
  );

  factory EntriesPage.fromJson(Map<String, dynamic> json) {
    final map = lowerKeys(json);
    return EntriesPage(
      items: asList(map['items']).map(Entry.fromJson).toList(),
      total: intOf(map, 'total'),
      page: intOf(map, 'page', 1),
      perPage: intOf(map, 'per_page', 25),
      pages: intOf(map, 'pages'),
    );
  }
}

/// One attributed name and how many ledger entries carry it.
class PersonCount {
  const PersonCount({required this.person, this.count = 0});

  final String person;
  final int count;

  factory PersonCount.fromJson(Map<String, dynamic> json) {
    final map = lowerKeys(json);
    return PersonCount(
      person: stringOf(map, 'person').trim(),
      count: intOf(map, 'count'),
    );
  }
}

/// Parses `GET /api/v1/persons` into a stable picker list.
///
/// The server already groups and orders case-insensitively; this repeats the
/// work so a fallback or an older server can never produce duplicate chips.
List<PersonCount> parsePersons(Object? payload) {
  final merged = <String, PersonCount>{};
  for (final row in asList(asMap(payload)['items'])) {
    final entry = PersonCount.fromJson(row);
    if (entry.person.isEmpty) continue;
    final key = entry.person.toLowerCase();
    final existing = merged[key];
    merged[key] = existing == null
        ? entry
        : PersonCount(
            person: existing.person,
            count: existing.count + entry.count,
          );
  }
  final persons = merged.values.toList()
    ..sort(
      (a, b) => a.person.toLowerCase().compareTo(b.person.toLowerCase()),
    );
  return persons;
}

/// Aggregate counters from `GET /api/v1/stats`.
class LedgerStats {
  const LedgerStats({
    required this.total,
    required this.uniqueWords,
    required this.entriesLast30Days,
    this.firstEntryAt,
    this.lastEntryAt,
  });

  final int total;
  final int uniqueWords;
  final int entriesLast30Days;
  final DateTime? firstEntryAt;
  final DateTime? lastEntryAt;

  static final LedgerStats empty = const LedgerStats(
    total: 0,
    uniqueWords: 0,
    entriesLast30Days: 0,
  );

  String get lastEntryLabel {
    final value = lastEntryAt;
    if (value == null) return '—';
    return DateFormat.yMMMd().format(value);
  }

  String get firstEntryLabel {
    final value = firstEntryAt;
    if (value == null) return '—';
    return DateFormat.yMMMd().format(value);
  }

  factory LedgerStats.fromJson(Map<String, dynamic> json) {
    final map = lowerKeys(json);
    return LedgerStats(
      total: intOf(map, 'total_entries'),
      uniqueWords: intOf(map, 'unique_words'),
      entriesLast30Days: intOf(map, 'entries_last_30_days'),
      firstEntryAt: _dateOf(map, 'first_entry_at'),
      lastEntryAt: _dateOf(map, 'last_entry_at'),
    );
  }
}
