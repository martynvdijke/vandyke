import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:vandyke/src/api/api_client.dart';
import 'package:vandyke/src/api/api_exception.dart';

void main() {
  test('normalizeServerUrl accepts bare hosts and API paths', () {
    expect(
      ApiClient.normalizeServerUrl('vandyke.thor.edu'),
      'https://vandyke.thor.edu',
    );
    expect(
      ApiClient.normalizeServerUrl('https://vandyke.thor.edu/'),
      'https://vandyke.thor.edu',
    );
    expect(
      ApiClient.normalizeServerUrl('http://localhost:8080/api/v1/'),
      'http://localhost:8080',
    );
    expect(ApiClient.normalizeServerUrl('  '), isEmpty);
  });

  test('entries sends search, sort, and page parameters', () async {
    late http.Request captured;
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com/api/v1/',
      client: MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'items': [
              {
                'id': 7,
                'word': 'Van Dyke',
                'meaning': 'Should be vanDyke',
                'person': 'Reception',
                'created_at': '2026-03-14T09:30:00Z',
              },
            ],
            'total': 41,
            'page': 2,
            'per_page': 25,
            'pages': 3,
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    final page = await client.entries(
      query: 'dyke',
      sort: 'word',
      order: 'asc',
      page: 2,
      perPage: 25,
    );

    expect(page.items.single.word, 'Van Dyke');
    expect(page.items.single.person, 'Reception');
    expect(page.total, 41);
    expect(page.page, 2);
    expect(page.pages, 3);
    expect(page.hasMore, isTrue);
    expect(captured.url.path, '/api/v1/entries');
    expect(captured.url.queryParameters['q'], 'dyke');
    expect(captured.url.queryParameters['sort'], 'word');
    expect(captured.url.queryParameters['order'], 'asc');
    expect(captured.url.queryParameters['page'], '2');
    expect(captured.url.queryParameters['per_page'], '25');
    expect(captured.url.queryParameters.containsKey('person'), isFalse);
    expect(captured.headers['Accept'], 'application/json');
  });

  test('entries sends an encoded person filter', () async {
    late http.Request captured;
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'items': <Object>[],
            'total': 0,
            'page': 1,
            'per_page': 25,
            'pages': 0,
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    final page = await client.entries(person: 'M. van Dijke & Co');

    expect(
      captured.url.queryParameters['person'],
      'M. van Dijke & Co',
    );
    // Round-trips through URL encoding without losing the ampersand.
    expect(captured.url.query, contains('person=M.+van+Dijke'));
    expect(page.items, isEmpty);
  });

  test('persons parses the distinct-name endpoint', () async {
    late http.Request captured;
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'items': [
              {'person': 'Concierge', 'count': 4},
              {'person': 'Reception', 'count': 12},
            ],
            'total': 2,
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    final persons = await client.persons();

    expect(captured.method, 'GET');
    expect(captured.url.path, '/api/v1/persons');
    expect(persons.map((person) => person.person), ['Concierge', 'Reception']);
    expect(persons.last.count, 12);
  });

  test('a missing persons endpoint is a typed not-found error', () async {
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'error': {'code': 'not_found', 'message': 'no such route'},
          }),
          404,
        ),
      ),
    );

    await expectLater(
      client.persons(),
      throwsA(
        isA<ApiException>().having((e) => e.isNotFound, 'isNotFound', isTrue),
      ),
    );
  });

  test('createEntry posts JSON and returns the created entry', () async {
    late http.Request captured;
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'id': 99,
            'word': 'Vandyck',
            'meaning': 'Should be vanDyke',
            'person': 'Concierge',
            'created_at': '2026-11-08T10:00:00Z',
          }),
          201,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    final entry = await client.createEntry(
      word: ' Vandyck ',
      meaning: 'Should be vanDyke',
      person: 'Concierge',
    );

    expect(captured.method, 'POST');
    expect(captured.url.path, '/api/v1/entries');
    expect(captured.headers['Content-Type'], startsWith('application/json'));
    final body = jsonDecode(captured.body) as Map<String, dynamic>;
    expect(body['word'], 'Vandyck');
    expect(body['meaning'], 'Should be vanDyke');
    expect(body['person'], 'Concierge');
    expect(entry.id, 99);
    expect(entry.word, 'Vandyck');
  });

  test('omits an empty person from the create payload', () async {
    late http.Request captured;
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'id': 100,
            'word': 'Vandike',
            'meaning': 'Should be vanDyke',
            'created_at': '2026-11-08T10:00:00Z',
          }),
          201,
        );
      }),
    );

    await client.createEntry(word: 'Vandike', meaning: 'Should be vanDyke');

    final body = jsonDecode(captured.body) as Map<String, dynamic>;
    expect(body.containsKey('person'), isFalse);
  });

  test('rate limiting becomes a friendly typed exception', () async {
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'error': {
              'code': 'rate_limited',
              'message': 'too many entries added; try again later',
            },
          }),
          429,
          headers: {'retry-after': '45'},
        ),
      ),
    );

    await expectLater(
      client.createEntry(word: 'Vandyke', meaning: 'Oops'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.isRateLimited, 'isRateLimited', isTrue)
            .having((e) => e.retryAfterSeconds, 'retryAfterSeconds', 45)
            .having(
              (e) => e.friendlyMessage,
              'friendlyMessage',
              contains('45 seconds'),
            ),
      ),
    );
  });

  test('validation errors preserve the server message', () async {
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'error': {
              'code': 'validation_error',
              'message': 'word must be at most 200 characters',
            },
          }),
          422,
        ),
      ),
    );

    await expectLater(
      client.createEntry(word: 'x', meaning: 'y'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.statusCode, 'statusCode', 422)
            .having((e) => e.code, 'code', 'validation_error')
            .having((e) => e.friendlyMessage, 'message', contains('200')),
      ),
    );
  });

  test('duplicate entries explain themselves', () async {
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'error': {
              'code': 'duplicate_entry',
              'message': 'that entry is already in the ledger',
            },
          }),
          409,
        ),
      ),
    );

    await expectLater(
      client.createEntry(word: 'Vandyke', meaning: 'Should be vanDyke'),
      throwsA(
        isA<ApiException>().having(
          (e) => e.friendlyMessage,
          'friendlyMessage',
          contains('already in the ledger'),
        ),
      ),
    );
  });

  test('unreachable hosts raise a clear connection error', () async {
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient(
        (_) async => throw http.ClientException('no route to host'),
      ),
    );

    await expectLater(
      client.stats(),
      throwsA(
        isA<ApiException>().having(
          (e) => e.message,
          'message',
          contains('Could not reach the ledger'),
        ),
      ),
    );
  });

  group('createEntry photo upload', () {
    late Directory dir;

    setUp(() async {
      dir = await Directory.systemTemp.createTemp('vandyke_upload_');
    });

    tearDown(() async {
      if (dir.existsSync()) await dir.delete(recursive: true);
    });

    test('posts multipart form data with the photo part', () async {
      final file = File('${dir.path}/evidence.jpg')
        ..writeAsBytesSync(List<int>.filled(64, 7));
      late http.Request captured;
      final progress = <String>[];
      final client = ApiClient(
        serverUrl: 'https://vandyke.example.com',
        client: MockClient((request) async {
          captured = request;
          return http.Response(
            jsonEncode({
              'id': 101,
              'word': 'Vandyck',
              'meaning': 'Should be vanDyke',
              'person': 'Reception',
              'photo_url': '/media/101.jpg',
              'created_at': '2026-11-08T10:00:00Z',
            }),
            201,
            headers: {'content-type': 'application/json'},
          );
        }),
      );

      final entry = await client.createEntry(
        word: ' Vandyck ',
        meaning: 'Should be vanDyke',
        person: 'Reception',
        photoPath: file.path,
        onProgress: (sent, total) => progress.add('$sent/$total'),
      );

      expect(captured.method, 'POST');
      expect(captured.url.path, '/api/v1/entries');
      expect(captured.headers['Accept'], 'application/json');
      expect(
        captured.headers['Content-Type'],
        startsWith('multipart/form-data; boundary='),
      );

      final body = utf8.decode(captured.bodyBytes);
      final lowerBody = body.toLowerCase();
      expect(body, contains('name="word"'));
      expect(body, contains('Vandyck'));
      expect(body, contains('name="meaning"'));
      expect(body, contains('Should be vanDyke'));
      expect(body, contains('name="person"'));
      expect(body, contains('Reception'));
      expect(body, contains('name="photo"'));
      expect(body, contains('filename="evidence.jpg"'));
      expect(lowerBody, contains('content-type: image/jpeg'));

      // The progress callback reports bytes written and the total body size.
      expect(progress, isNotEmpty);
      final sent = int.parse(progress.last.split('/').first);
      final total = int.parse(progress.last.split('/').last);
      expect(total, greaterThan(0));
      expect(sent, total);

      expect(entry.id, 101);
      expect(entry.hasPhoto, isTrue);
    });

    test('keeps the JSON body when no photo is attached', () async {
      late http.Request captured;
      final client = ApiClient(
        serverUrl: 'https://vandyke.example.com',
        client: MockClient((request) async {
          captured = request;
          return http.Response(
            jsonEncode({
              'id': 102,
              'word': 'Vandike',
              'meaning': 'Should be vanDyke',
              'created_at': '2026-11-08T10:00:00Z',
            }),
            201,
          );
        }),
      );

      await client.createEntry(
        word: 'Vandike',
        meaning: 'Should be vanDyke',
        onProgress: (_, _) => fail('progress belongs to the multipart path'),
      );

      expect(captured.headers['Content-Type'], startsWith('application/json'));
      final body = jsonDecode(captured.body) as Map<String, dynamic>;
      expect(body['word'], 'Vandike');
    });
  });

  test('a 413 response becomes the image-too-large message', () async {
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'error': {
              'code': 'request_too_large',
              'message': 'request body too large',
            },
          }),
          413,
        ),
      ),
    );

    await expectLater(
      client.createEntry(word: 'Vandyke', meaning: 'Oops'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.isPayloadTooLarge, 'isPayloadTooLarge', isTrue)
            .having(
              (e) => e.friendlyMessage,
              'friendlyMessage',
              contains('image is too large'),
            ),
      ),
    );
  });

  test('entryById resolves GET /api/v1/entries/{id}', () async {
    late http.Request captured;
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'id': 42,
            'word': 'Van Dike',
            'meaning': 'Should be vanDyke',
            'created_at': '2026-03-14T09:30:00Z',
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    final entry = await client.entryById(42);

    expect(captured.method, 'GET');
    expect(captured.url.path, '/api/v1/entries/42');
    expect(entry.id, 42);
    expect(entry.word, 'Van Dike');
  });

  test('entryById surfaces a 404 as a typed not-found', () async {
    final client = ApiClient(
      serverUrl: 'https://vandyke.example.com',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'error': {'code': 'not_found', 'message': 'no such entry'},
          }),
          404,
        ),
      ),
    );

    await expectLater(
      client.entryById(999),
      throwsA(
        isA<ApiException>().having((e) => e.isNotFound, 'isNotFound', isTrue),
      ),
    );
  });
}
