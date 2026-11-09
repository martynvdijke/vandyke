import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:vandyke/src/api/api_client.dart';
import 'package:vandyke/src/api/models.dart';
import 'package:vandyke/src/config.dart';
import 'package:vandyke/src/providers.dart';
import 'package:vandyke/src/screens/ledger_screen.dart';
import 'package:vandyke/src/screens/setup_screen.dart';
import 'package:vandyke/src/screens/stats_screen.dart';
import 'package:vandyke/src/storage.dart';
import 'package:vandyke/src/theme/tokens.dart';
import 'package:vandyke/src/theme/vandyke_theme.dart';
import 'package:vandyke/src/widgets/person_filter_bar.dart';
import 'package:vandyke/src/widgets/recent_entries_strip.dart';

void main() {
  test('the theme carries the editorial palette', () {
    final theme = VandykeTheme.light();
    expect(theme.colorScheme.primary, VandykeColors.green);
    expect(theme.scaffoldBackgroundColor, VandykeColors.parchment);
    expect(theme.textTheme.displaySmall?.fontFamily, 'Newsreader');
    expect(theme.textTheme.bodyMedium?.fontFamily, 'DMSans');
  });

  test('the default server is used until one is saved', () {
    expect(defaultServerUrl, 'https://vandyke.thor.edu');
    expect(resolveInitialServerUrl(null), defaultServerUrl);
    expect(resolveInitialServerUrl('  '), defaultServerUrl);
    expect(
      resolveInitialServerUrl('https://ledger.example.com'),
      'https://ledger.example.com',
    );
  });

  testWidgets('setup prefills the default ledger address', (tester) async {
    final container = ProviderContainer(
      overrides: [appStorageProvider.overrideWithValue(_FakeStorage())],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          theme: VandykeTheme.light(),
          home: const SetupScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final field = tester.widget<TextField>(find.byType(TextField).first);
    expect(field.controller?.text, defaultServerUrl);
    expect(find.text('Open the ledger'), findsOneWidget);
  });

  testWidgets('ledger renders entries, stats, and the latest slips strip', (
    tester,
  ) async {
    _useTallView(tester);
    final ledger = _FakeLedger();
    final container = await _pumpLedger(tester, ledger);

    expect(find.text('LATEST SLIPS'), findsOneWidget);
    expect(find.byType(RecentEntriesStrip), findsOneWidget);
    // Each of the newest two appears in both the strip and its card.
    expect(find.text('Van Dyke'), findsNWidgets(2));
    expect(find.text('Vandike'), findsNWidgets(2));
    expect(find.text('2 entries on file'), findsOneWidget);
    expect(container.read(ledgerProvider).items.length, 2);
  });

  testWidgets('person chips come from the persons endpoint', (tester) async {
    _useTallView(tester);
    final ledger = _FakeLedger();
    await _pumpLedger(tester, ledger);

    final bar = find.byType(PersonFilterBar);
    expect(bar, findsOneWidget);
    expect(
      find.descendant(of: bar, matching: find.text('Everyone')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: bar, matching: find.text('Reception')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: bar, matching: find.text('Concierge')),
      findsOneWidget,
    );
    expect(ledger.requests.any((uri) => uri.path.endsWith('/persons')), isTrue);
  });

  testWidgets('choosing a person re-queries the ledger with the filter', (
    tester,
  ) async {
    _useTallView(tester);
    final ledger = _FakeLedger();
    final container = await _pumpLedger(tester, ledger);

    await tester.tap(
      find.descendant(
        of: find.byType(PersonFilterBar),
        matching: find.text('Reception'),
      ),
    );
    await tester.pumpAndSettle();

    final filtered = ledger.entriesRequests.last;
    expect(filtered.queryParameters['person'], 'Reception');
    expect(filtered.queryParameters['page'], '1');
    expect(container.read(ledgerProvider).person, 'Reception');
    expect(find.text('1 slip by Reception'), findsOneWidget);
    // Only Reception's slip survives; the Concierge-only word is filtered out
    // of both the feed and the now-filtered latest strip.
    expect(find.text('Van Dyke'), findsWidgets);
    expect(find.text('Vandike'), findsNothing);

    // "Everyone" clears the filter and restores the full feed.
    await tester.tap(
      find.descendant(
        of: find.byType(PersonFilterBar),
        matching: find.text('Everyone'),
      ),
    );
    await tester.pumpAndSettle();
    expect(container.read(ledgerProvider).person, isEmpty);
    expect(find.text('Vandike'), findsWidgets);
  });

  testWidgets('a person with no slips gets a specific empty state', (
    tester,
  ) async {
    _useTallView(tester);
    final ledger = _FakeLedger(
      personsOverride: const [
        {'person': 'M. van Dijke', 'count': 4},
      ],
    );
    await _pumpLedger(tester, ledger);

    await tester.tap(
      find.descendant(
        of: find.byType(PersonFilterBar),
        matching: find.text('M. van Dijke'),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('No slips recorded by M. van Dijke yet.'), findsOneWidget);
    expect(find.text('Show everyone'), findsOneWidget);
  });

  testWidgets('a search inside a person filter explains the combination', (
    tester,
  ) async {
    _useTallView(tester);
    final ledger = _FakeLedger();
    await _pumpLedger(tester, ledger);

    await tester.tap(
      find.descendant(
        of: find.byType(PersonFilterBar),
        matching: find.text('Reception'),
      ),
    );
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), 'nothing-here');
    await tester.pump(const Duration(milliseconds: 400));
    await tester.pumpAndSettle();

    expect(
      find.text('Nothing filed by Reception matches that search.'),
      findsOneWidget,
    );
    expect(find.text('Clear filters'), findsOneWidget);
  });

  testWidgets('the person picker falls back when /persons is missing', (
    tester,
  ) async {
    _useTallView(tester);
    final ledger = _FakeLedger()..personsAvailable = false;
    await _pumpLedger(tester, ledger);

    // The chip row is rebuilt from the entries already on screen.
    expect(
      find.descendant(
        of: find.byType(PersonFilterBar),
        matching: find.text('Reception'),
      ),
      findsOneWidget,
    );
    expect(find.text('2 entries on file'), findsOneWidget);
  });

  testWidgets('typing debounces into a server-side search', (tester) async {
    _useTallView(tester);
    final ledger = _FakeLedger();
    final container = await _pumpLedger(tester, ledger);

    await tester.enterText(find.byType(TextField), 'dyke');
    await tester.pump(const Duration(milliseconds: 200));
    // Not sent before the debounce elapses.
    expect(
      ledger.entriesRequests.where((uri) => uri.queryParameters['q'] == 'dyke'),
      isEmpty,
    );

    await tester.pump(const Duration(milliseconds: 250));
    await tester.pumpAndSettle();
    expect(
      ledger.entriesRequests.where((uri) => uri.queryParameters['q'] == 'dyke'),
      isNotEmpty,
    );
    expect(container.read(ledgerProvider).query, 'dyke');
  });

  testWidgets('the latest slips strip follows a new submission', (
    tester,
  ) async {
    _useTallView(tester);
    final ledger = _FakeLedger();
    final container = await _pumpLedger(tester, ledger);
    final before = ledger.recentRequests.length;

    ledger.entries.insert(0, {
      'id': 3,
      'word': 'VanDyck',
      'meaning': 'Should read vanDyke',
      'person': 'Front desk',
      'created_at': '2026-11-08T10:00:00Z',
    });
    await container
        .read(ledgerProvider.notifier)
        .acceptCreated(Entry.fromJson(ledger.entries.first));
    await tester.pumpAndSettle();

    expect(ledger.recentRequests.length, greaterThan(before));
    expect(find.text('VanDyck'), findsWidgets);
    expect(container.read(ledgerProvider).items.first.word, 'VanDyck');
  });

  testWidgets('tapping a latest slip opens the entry dossier', (tester) async {
    _useTallView(tester);
    final ledger = _FakeLedger();
    await _pumpLedger(tester, ledger);

    await tester.tap(
      find.descendant(
        of: find.byType(RecentEntriesStrip),
        matching: find.text('Van Dyke'),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('LEDGER ENTRY · NO. 1'), findsOneWidget);
    expect(find.text('Attributed to'), findsOneWidget);
  });

  testWidgets('stats renders headline figures and the breakdowns', (
    tester,
  ) async {
    _useTallView(tester);
    final ledger = _FakeLedger(
      statsPayload: {
        'total_entries': 214,
        'unique_words': 63,
        'entries_last_30_days': 9,
        'with_photo': 42,
        'by_year': [
          {'year': 2019, 'count': 3},
          {'year': 2026, 'count': 40},
        ],
        'top_persons': [
          {'person': 'Reception', 'count': 84},
          {'person': 'Concierge', 'count': 31},
        ],
        'first_entry_at': '2019-02-03T00:00:00Z',
        'last_entry_at': '2026-11-07T12:00:00Z',
      },
    );
    await _pumpStats(tester, ledger);

    expect(find.text('214'), findsOneWidget);
    expect(find.text('20%'), findsOneWidget);
    expect(find.text('42 of 214 slips'), findsOneWidget);
    expect(find.text('Running for 7 years, 9 months.'), findsOneWidget);
    expect(find.text('SLIPS PER YEAR'), findsOneWidget);
    expect(find.text('2019–2026'), findsOneWidget);
    expect(find.text('2019'), findsOneWidget);
    expect(find.text('MOST FREQUENT AUTHORS'), findsOneWidget);
    expect(find.text('Reception'), findsOneWidget);
    expect(find.text('84'), findsOneWidget);
    expect(find.text('Nothing counted yet'), findsNothing);
  });

  testWidgets('stats hides breakdowns an older server omits', (tester) async {
    _useTallView(tester);
    final ledger = _FakeLedger(
      statsPayload: {
        'total_entries': 12,
        'unique_words': 7,
        'entries_last_30_days': 2,
        'first_entry_at': '2024-01-01T00:00:00Z',
        'last_entry_at': '2026-01-01T00:00:00Z',
      },
    );
    await _pumpStats(tester, ledger);

    expect(find.text('12'), findsOneWidget);
    expect(find.text('SLIPS PER YEAR'), findsNothing);
    expect(find.text('MOST FREQUENT AUTHORS'), findsNothing);
    expect(find.text('WITH A PHOTO'), findsNothing);
    expect(find.text('UNIQUE SPELLINGS'), findsOneWidget);
    expect(find.text('Running for 2 years.'), findsOneWidget);
  });

  testWidgets('stats invites a fresh ledger to start', (tester) async {
    _useTallView(tester);
    final ledger = _FakeLedger(
      statsPayload: {
        'total_entries': 0,
        'unique_words': 0,
        'entries_last_30_days': 0,
      },
    );
    await _pumpStats(tester, ledger);

    expect(find.text('Nothing counted yet'), findsOneWidget);
    expect(find.text('SLIPS PER YEAR'), findsNothing);
    expect(find.text('214'), findsNothing);
  });

  testWidgets('stats pull-to-refresh asks the server again', (tester) async {
    _useTallView(tester);
    final ledger = _FakeLedger(
      statsPayload: {
        'total_entries': 12,
        'unique_words': 7,
        'entries_last_30_days': 2,
      },
    );
    await _pumpStats(tester, ledger);
    final before = ledger.requests
        .where((uri) => uri.path.endsWith('/stats'))
        .length;

    await tester.fling(find.byType(ListView), const Offset(0, 420), 1200);
    await tester.pumpAndSettle();

    final after = ledger.requests
        .where((uri) => uri.path.endsWith('/stats'))
        .length;
    expect(after, greaterThan(before));
  });
}

/// The feed plus the recent strip need room; the default test surface is
/// 800×600, which would push the cards out of the viewport.
void _useTallView(WidgetTester tester) {
  tester.view.physicalSize = const Size(900, 1600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

Future<ProviderContainer> _pumpLedger(
  WidgetTester tester,
  _FakeLedger ledger,
) async {
  return _pumpScreen(tester, ledger, const LedgerScreen());
}

Future<ProviderContainer> _pumpStats(
  WidgetTester tester,
  _FakeLedger ledger,
) async {
  return _pumpScreen(tester, ledger, const StatsScreen());
}

Future<ProviderContainer> _pumpScreen(
  WidgetTester tester,
  _FakeLedger ledger,
  Widget home,
) async {
  final client = ApiClient(
    serverUrl: 'https://ledger.example.com',
    client: MockClient(ledger.handle),
  );
  final container = ProviderContainer(
    overrides: [apiClientProvider.overrideWithValue(client)],
  );
  addTearDown(container.dispose);

  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp(theme: VandykeTheme.light(), home: home),
    ),
  );
  await tester.pumpAndSettle();
  return container;
}

/// An in-memory stand-in for the ledger API: filtering, pagination, person
/// counts, and the optional `/persons` endpoint all behave like the server.
class _FakeLedger {
  _FakeLedger({
    List<Map<String, Object?>>? entries,
    this.personsOverride,
    this.statsPayload,
  }) : entries =
           entries ??
           [
             {
               'id': 1,
               'word': 'Van Dyke',
               'meaning': 'Should read vanDyke',
               'person': 'Reception',
               'created_at': '2026-03-14T09:30:00Z',
             },
             {
               'id': 2,
               'word': 'Vandike',
               'meaning': 'Should read vanDyke',
               'person': 'Concierge',
               'created_at': '2026-03-01T11:00:00Z',
             },
           ];

  final List<Map<String, Object?>> entries;
  final List<Map<String, Object?>>? personsOverride;

  /// When set, `/stats` answers with exactly this payload (used to exercise
  /// newer and older server shapes).
  final Map<String, Object?>? statsPayload;
  final List<Uri> requests = [];
  bool personsAvailable = true;

  List<Uri> get entriesRequests =>
      requests.where((uri) => uri.path.endsWith('/entries')).toList();

  List<Uri> get recentRequests => entriesRequests
      .where((uri) => uri.queryParameters['per_page'] == '5')
      .toList();

  Future<http.Response> handle(http.Request request) async {
    requests.add(request.url);
    final path = request.url.path;
    if (path.endsWith('/stats')) return _stats();
    if (path.endsWith('/persons')) return _persons();
    return _entries(request.url);
  }

  http.Response _stats() {
    final payload = statsPayload;
    if (payload != null) return _json(payload);
    final words = entries.map((item) => '${item['word']}').toSet();
    return _json({
      'total_entries': entries.length,
      'unique_words': words.length,
      'entries_last_30_days': entries.length,
      'first_entry_at': '2026-03-01T00:00:00Z',
      'last_entry_at': entries.isEmpty ? null : entries.first['created_at'],
    });
  }

  http.Response _persons() {
    if (!personsAvailable) {
      return _json({
        'error': {'code': 'not_found', 'message': 'no such route'},
      }, 404);
    }
    if (personsOverride != null) {
      return _json({
        'items': personsOverride,
        'total': personsOverride!.length,
      });
    }
    final counts = <String, Map<String, Object?>>{};
    for (final entry in entries) {
      final person = '${entry['person'] ?? ''}'.trim();
      if (person.isEmpty) continue;
      final key = person.toLowerCase();
      counts[key] = {
        'person': counts[key]?['person'] ?? person,
        'count': ((counts[key]?['count'] as int?) ?? 0) + 1,
      };
    }
    final items = counts.values.toList()
      ..sort(
        (a, b) => '${a['person']}'.toLowerCase().compareTo(
          '${b['person']}'.toLowerCase(),
        ),
      );
    return _json({'items': items, 'total': items.length});
  }

  http.Response _entries(Uri uri) {
    final query = (uri.queryParameters['q'] ?? '').toLowerCase().trim();
    final person = (uri.queryParameters['person'] ?? '').toLowerCase().trim();
    final matches = entries.where((item) {
      final word = '${item['word']}'.toLowerCase();
      final owner = '${item['person'] ?? ''}'.toLowerCase();
      if (query.isNotEmpty && !word.contains(query)) return false;
      if (person.isNotEmpty && owner != person) return false;
      return true;
    }).toList();

    if (uri.queryParameters['sort'] == 'word') {
      matches.sort(
        (a, b) => '${a['word']}'.toLowerCase().compareTo(
          '${b['word']}'.toLowerCase(),
        ),
      );
    } else {
      matches.sort(
        (a, b) => '${b['created_at']}'.compareTo('${a['created_at']}'),
      );
    }

    final perPage = int.tryParse(uri.queryParameters['per_page'] ?? '') ?? 25;
    final page = int.tryParse(uri.queryParameters['page'] ?? '') ?? 1;
    final start = (page - 1) * perPage;
    final items = start >= matches.length
        ? const <Map<String, Object?>>[]
        : matches.skip(start).take(perPage).toList();
    return _json({
      'items': items,
      'total': matches.length,
      'page': page,
      'per_page': perPage,
      'pages': matches.isEmpty ? 0 : (matches.length / perPage).ceil(),
    });
  }

  http.Response _json(Object payload, [int status = 200]) {
    return http.Response(
      jsonEncode(payload),
      status,
      headers: {'content-type': 'application/json'},
    );
  }
}

class _FakeStorage extends AppStorage {
  @override
  Future<String?> readServer() async => null;

  @override
  Future<String> readTheme() async => 'system';
}
