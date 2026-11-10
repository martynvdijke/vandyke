import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:image_picker/image_picker.dart';
import 'package:vandyke/src/api/api_client.dart';
import 'package:vandyke/src/api/models.dart';
import 'package:vandyke/src/photo.dart';
import 'package:vandyke/src/providers.dart';
import 'package:vandyke/src/share.dart';
import 'package:vandyke/src/theme/vandyke_theme.dart';
import 'package:vandyke/src/widgets/entry_detail_sheet.dart';
import 'package:vandyke/src/widgets/log_misspelling_sheet.dart';

/// A 1x1 PNG, enough for the preview to decode inside a widget test.
const _pngPixel =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQ'
    'DwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

const _previewKey = ValueKey('photo-attachment-preview');

Future<ProviderContainer> _pumpSheet(
  WidgetTester tester,
  Widget child, {
  PhotoPicker? picker,
  CameraPermissionRequest? cameraPermission,
  UrlSharer? sharer,
  ApiClient? client,
}) async {
  final api =
      client ??
      ApiClient(
        serverUrl: 'https://ledger.example.com',
        client: MockClient((_) async => http.Response('{}', 200)),
      );
  final container = ProviderContainer(
    overrides: [
      apiClientProvider.overrideWithValue(api),
      if (picker != null) photoPickerProvider.overrideWithValue(picker),
      if (cameraPermission != null)
        cameraPermissionProvider.overrideWithValue(cameraPermission),
      if (sharer != null) urlSharerProvider.overrideWithValue(sharer),
    ],
  );
  addTearDown(container.dispose);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp(
        theme: VandykeTheme.light(),
        home: Scaffold(body: child),
      ),
    ),
  );
  await tester.pumpAndSettle();
  return container;
}

void main() {
  late Directory dir;
  late File photoFile;

  setUp(() async {
    dir = await Directory.systemTemp.createTemp('vandyke_photo_');
    photoFile = File('${dir.path}/evidence.png')
      ..writeAsBytesSync(base64Decode(_pngPixel));
  });

  tearDown(() async {
    if (dir.existsSync()) await dir.delete(recursive: true);
  });

  testWidgets('attaching a gallery photo previews it and removal clears it', (
    tester,
  ) async {
    await _pumpSheet(
      tester,
      const LogMisspellingSheet(),
      picker: (source) async => XFile(photoFile.path),
    );

    expect(find.byKey(_previewKey), findsNothing);

    await tester.tap(find.byKey(const ValueKey('photo-gallery')));
    await tester.pumpAndSettle();

    expect(find.byKey(_previewKey), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('photo-remove')));
    await tester.pumpAndSettle();

    expect(find.byKey(_previewKey), findsNothing);
    // The sheet returns to a text-only submission.
    expect(find.text('PHOTO EVIDENCE (OPTIONAL)'), findsOneWidget);
    expect(find.text('Log a new misspelling'), findsOneWidget);
  });

  testWidgets('a cancelled picker leaves the sheet text-only', (tester) async {
    await _pumpSheet(
      tester,
      const LogMisspellingSheet(),
      picker: (source) async => null,
    );

    await tester.tap(find.byKey(const ValueKey('photo-gallery')));
    await tester.pumpAndSettle();

    expect(find.byKey(_previewKey), findsNothing);
    expect(find.text('PHOTO EVIDENCE (OPTIONAL)'), findsOneWidget);
  });

  testWidgets('a denied camera permission explains itself and keeps state', (
    tester,
  ) async {
    await _pumpSheet(
      tester,
      const LogMisspellingSheet(),
      cameraPermission: () async => false,
      picker: (source) async =>
          fail('the picker must not open without permission'),
    );

    await tester.enterText(find.byType(TextFormField).first, 'Vandyck');
    await tester.tap(find.byKey(const ValueKey('photo-camera')));
    await tester.pumpAndSettle();

    expect(find.byKey(_previewKey), findsNothing);
    expect(find.textContaining('Camera access is off'), findsOneWidget);
    expect(find.text('Vandyck'), findsOneWidget);
  });

  testWidgets('the detail sheet shares the entry permalink', (tester) async {
    String? shared;
    String? sharedSubject;
    final entry = Entry.fromJson({
      'id': 42,
      'word': 'Van Dike',
      'meaning': 'Should be vanDyke',
      'person': 'Reception',
      'created_at': '2026-11-08T10:00:00Z',
    });
    await _pumpSheet(
      tester,
      EntryDetailSheet(entry: entry),
      sharer: (String url, {String subject = ''}) async {
        shared = url;
        sharedSubject = subject;
      },
    );

    await tester.tap(find.byKey(const ValueKey('entry-share')));
    await tester.pumpAndSettle();

    expect(shared, 'https://ledger.example.com/e/42');
    expect(sharedSubject, contains('42'));
  });

  testWidgets('the detail sheet resolves a linked id by request', (
    tester,
  ) async {
    final requested = <Uri>[];
    final client = ApiClient(
      serverUrl: 'https://ledger.example.com',
      client: MockClient((request) async {
        requested.add(request.url);
        return http.Response(
          jsonEncode({
            'id': 77,
            'word': 'Vandyke',
            'meaning': 'Should be vanDyke',
            'created_at': '2026-11-08T10:00:00Z',
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );
    await _pumpSheet(
      tester,
      const EntryDetailSheet(entryId: 77),
      client: client,
    );

    expect(requested.single.path, '/api/v1/entries/77');
    expect(find.text('Vandyke'), findsOneWidget);
    expect(find.text('LEDGER ENTRY · NO. 77'), findsOneWidget);
  });

  testWidgets('the detail sheet shows a not-found message on 404', (
    tester,
  ) async {
    final client = ApiClient(
      serverUrl: 'https://ledger.example.com',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'error': {'code': 'not_found', 'message': 'no such entry'},
          }),
          404,
        ),
      ),
    );
    await _pumpSheet(
      tester,
      const EntryDetailSheet(entryId: 999),
      client: client,
    );

    expect(find.text('That ledger entry could not be found.'), findsOneWidget);
    expect(find.text('Try again'), findsNothing);
  });
}
