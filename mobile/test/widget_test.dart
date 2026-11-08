import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:vandyke/src/api/api_client.dart';
import 'package:vandyke/src/config.dart';
import 'package:vandyke/src/providers.dart';
import 'package:vandyke/src/screens/ledger_screen.dart';
import 'package:vandyke/src/screens/setup_screen.dart';
import 'package:vandyke/src/storage.dart';
import 'package:vandyke/src/theme/tokens.dart';
import 'package:vandyke/src/theme/vandyke_theme.dart';

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

  testWidgets('ledger renders entries and stats from the API', (tester) async {
    final client = ApiClient(
      serverUrl: 'https://ledger.example.com',
      client: MockClient(_ledgerResponse),
    );
    final container = ProviderContainer(
      overrides: [apiClientProvider.overrideWithValue(client)],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          theme: VandykeTheme.light(),
          home: const LedgerScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Van Dyke'), findsOneWidget);
    expect(find.text('Vandyck'), findsOneWidget);
    expect(find.text('Should read vanDyke'), findsNWidgets(2));
    expect(find.text('2 entries on file'), findsOneWidget);
  });

  testWidgets('typing debounces into a server-side search', (tester) async {
    final queries = <String?>[];
    final client = ApiClient(
      serverUrl: 'https://ledger.example.com',
      client: MockClient((request) async {
        queries.add(request.url.queryParameters['q']);
        return _ledgerResponse(request);
      }),
    );
    final container = ProviderContainer(
      overrides: [apiClientProvider.overrideWithValue(client)],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          theme: VandykeTheme.light(),
          home: const LedgerScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), 'dyke');
    await tester.pump(const Duration(milliseconds: 200));
    // Not sent before the debounce elapses.
    expect(queries.where((q) => q == 'dyke'), isEmpty);

    await tester.pump(const Duration(milliseconds: 250));
    await tester.pumpAndSettle();
    expect(queries, contains('dyke'));
  });
}

Future<http.Response> _ledgerResponse(http.Request request) async {
  if (request.url.path.endsWith('/stats')) {
    return http.Response(
      jsonEncode({
        'total_entries': 2,
        'unique_words': 2,
        'entries_last_30_days': 2,
        'first_entry_at': '2026-03-01T00:00:00Z',
        'last_entry_at': '2026-03-14T09:30:00Z',
      }),
      200,
      headers: {'content-type': 'application/json'},
    );
  }
  final query = request.url.queryParameters['q'] ?? '';
  final items =
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
              'word': 'Vandyck',
              'meaning': 'Should read vanDyke',
              'created_at': '2026-03-01T11:00:00Z',
            },
          ]
          .where((item) => query.isEmpty || '${item['word']}'.contains(query))
          .toList();
  return http.Response(
    jsonEncode({
      'items': items,
      'total': query.isEmpty ? 2 : items.length,
      'page': 1,
      'per_page': 25,
      'pages': 1,
    }),
    200,
    headers: {'content-type': 'application/json'},
  );
}

class _FakeStorage extends AppStorage {
  @override
  Future<String?> readServer() async => null;

  @override
  Future<String> readTheme() async => 'system';
}
