import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import 'api_exception.dart';
import 'models.dart';

/// Thin, injectable wrapper around the public `/api/v1` ledger contract.
class ApiClient {
  ApiClient({required String serverUrl, http.Client? client})
    : baseUrl = normalizeServerUrl(serverUrl),
      _client = client ?? http.Client();

  final String baseUrl;
  final http.Client _client;

  static const Duration _timeout = Duration(seconds: 20);

  /// Accepts bare hosts and stray paths; the app always talks to the root.
  static String normalizeServerUrl(String raw) {
    var value = raw.trim();
    while (value.endsWith('/')) {
      value = value.substring(0, value.length - 1);
    }
    if (value.isEmpty) return value;
    if (!value.startsWith('http://') && !value.startsWith('https://')) {
      value = 'https://$value';
    }
    if (value.endsWith('/api/v1')) {
      value = value.substring(0, value.length - 7);
    }
    while (value.endsWith('/')) {
      value = value.substring(0, value.length - 1);
    }
    return value;
  }

  Uri _uri(String path, [Map<String, String>? query]) {
    final uri = Uri.parse('$baseUrl/api/v1$path');
    if (query == null || query.isEmpty) return uri;
    return uri.replace(queryParameters: {...uri.queryParameters, ...query});
  }

  /// Confirms the address speaks the vandyke API before it is saved.
  Future<LedgerStats> checkServer() => stats();

  Future<LedgerStats> stats() async {
    final decoded = await _send('GET', '/stats', route: '/stats');
    return LedgerStats.fromJson(decoded);
  }

  Future<EntriesPage> entries({
    String query = '',
    String person = '',
    String sort = 'created_at',
    String order = 'desc',
    int page = 1,
    int perPage = 25,
  }) async {
    final params = <String, String>{
      'page': '$page',
      'per_page': '$perPage',
      'sort': sort,
      'order': order,
      if (query.trim().isNotEmpty) 'q': query.trim(),
      if (person.trim().isNotEmpty) 'person': person.trim(),
    };
    final decoded = await _send(
      'GET',
      '/entries',
      query: params,
      route: '/entries',
    );
    return EntriesPage.fromJson(decoded);
  }

  /// Distinct attributed persons with their entry counts.
  ///
  /// Optional on the server: callers should tolerate a failure and fall back
  /// to the persons already present in the feed.
  Future<List<PersonCount>> persons() async {
    final decoded = await _send('GET', '/persons', route: '/persons');
    return parsePersons(decoded);
  }

  Future<Entry> createEntry({
    required String word,
    required String meaning,
    String person = '',
  }) async {
    final decoded = await _send(
      'POST',
      '/entries',
      body: {
        'word': word.trim(),
        'meaning': meaning.trim(),
        if (person.trim().isNotEmpty) 'person': person.trim(),
      },
      route: '/entries',
    );
    return Entry.fromJson(decoded);
  }

  Future<Map<String, dynamic>> _send(
    String method,
    String path, {
    Map<String, String>? query,
    Map<String, Object?>? body,
    required String route,
  }) async {
    final headers = <String, String>{
      'Accept': 'application/json',
      if (body != null) 'Content-Type': 'application/json',
    };
    try {
      final request = http.Request(method, _uri(path, query))
        ..headers.addAll(headers);
      if (body != null) request.body = jsonEncode(body);
      final streamed = await _client.send(request).timeout(_timeout);
      final response = await http.Response.fromStream(streamed);
      final decoded = _decode(response.body);

      if (response.statusCode < 200 || response.statusCode >= 300) {
        final error = asMap(decoded['error']);
        throw ApiException(
          stringOf(error, 'message', _fallbackMessage(response.statusCode)),
          statusCode: response.statusCode,
          code: stringOf(error, 'code', 'request_failed'),
          retryAfterSeconds: _retryAfter(response.headers['retry-after']),
        );
      }
      return decoded;
    } on ApiException {
      rethrow;
    } on TimeoutException {
      throw const ApiException(
        'The ledger took too long to answer. Check the address and try again.',
      );
    } on http.ClientException {
      throw const ApiException(
        'Could not reach the ledger. Check the address and your connection.',
      );
    } catch (_) {
      throw const ApiException(
        'Could not reach the ledger. Check the address and your connection.',
      );
    }
  }

  static String _fallbackMessage(int statusCode) {
    if (statusCode == 429) return 'Too many entries added; try again later.';
    if (statusCode == 409) return 'That entry is already in the ledger.';
    return 'That request could not be completed.';
  }

  static int? _retryAfter(String? raw) {
    if (raw == null) return null;
    final seconds = int.tryParse(raw.trim());
    if (seconds != null) return seconds;
    final date = DateTime.tryParse(raw);
    if (date == null) return null;
    final delta = date.difference(DateTime.now()).inSeconds;
    return delta > 0 ? delta : null;
  }

  static Map<String, dynamic> _decode(String body) {
    if (body.trim().isEmpty) return const {};
    try {
      final decoded = jsonDecode(body);
      return decoded is Map ? lowerKeys(decoded) : const {};
    } on FormatException {
      return const {};
    }
  }

  void close() => _client.close();
}
