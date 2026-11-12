import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:http_parser/http_parser.dart';

import '../notifications/push_config.dart';
import 'api_exception.dart';
import 'models.dart';

/// Reports how much of a request body has been written, in bytes.
typedef UploadProgressCallback = void Function(int bytesSent, int bytesTotal);

/// A multipart request whose body stream reports how much has been written.
///
/// `http.MultipartRequest` finalizes its body without exposing progress, so
/// the stream is wrapped here to count the bytes as the client consumes them.
class ProgressMultipartRequest extends http.MultipartRequest {
  ProgressMultipartRequest(
    super.method,
    super.url, {
    this.onProgress,
  });

  final UploadProgressCallback? onProgress;

  @override
  http.ByteStream finalize() {
    final byteStream = super.finalize();
    final onProgress = this.onProgress;
    if (onProgress == null) return byteStream;

    final total = contentLength;
    var sent = 0;
    return http.ByteStream(
      byteStream.transform(
        StreamTransformer<List<int>, List<int>>.fromHandlers(
          handleData: (chunk, sink) {
            sent += chunk.length;
            onProgress(sent, total);
            sink.add(chunk);
          },
        ),
      ),
    );
  }
}

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
    String? photoPath,
    UploadProgressCallback? onProgress,
  }) async {
    final trimmedWord = word.trim();
    final trimmedMeaning = meaning.trim();
    final trimmedPerson = person.trim();
    final photo = photoPath?.trim() ?? '';
    if (photo.isEmpty) {
      final decoded = await _send(
        'POST',
        '/entries',
        body: {
          'word': trimmedWord,
          'meaning': trimmedMeaning,
          if (trimmedPerson.isNotEmpty) 'person': trimmedPerson,
        },
        route: '/entries',
      );
      return Entry.fromJson(decoded);
    }

    final request = ProgressMultipartRequest(
      'POST',
      _uri('/entries'),
      onProgress: onProgress,
    )
      ..fields['word'] = trimmedWord
      ..fields['meaning'] = trimmedMeaning;
    if (trimmedPerson.isNotEmpty) request.fields['person'] = trimmedPerson;
    request.files.add(
      await http.MultipartFile.fromPath(
        'photo',
        photo,
        contentType: MediaType('image', 'jpeg'),
      ),
    );
    final decoded = await _dispatch(request, route: '/entries');
    return Entry.fromJson(decoded);
  }

  /// Resolves one entry by id, so a permalink or deep link never depends on
  /// the entry sitting in the currently loaded page.
  Future<Entry> entryById(int id) async {
    final decoded = await _send('GET', '/entries/$id', route: '/entries/{id}');
    return Entry.fromJson(decoded);
  }

  /// The Firebase client configuration for this server. Servers without push
  /// answer with `enabled: false`, which is not an error.
  Future<PushConfig> pushConfig() async {
    final decoded = await _send('GET', '/push/config', route: '/push/config');
    return PushConfig.fromJson(decoded);
  }

  /// Registers (or refreshes) this device's push token. The server answers
  /// `201 {"id": ...}`; this device only needs the token to stay current.
  Future<void> registerDevice({
    required String token,
    required String platform,
    required String deviceName,
    required String appVersion,
  }) async {
    await _send(
      'POST',
      '/push/devices',
      body: {
        'token': token,
        'platform': platform,
        'device_name': deviceName,
        'app_version': appVersion,
      },
      route: '/push/devices',
    );
  }

  /// Forgets a push token, so the device stops receiving alerts.
  Future<void> unregisterDevice(String token) async {
    await _send(
      'DELETE',
      '/push/devices',
      body: {'token': token},
      route: '/push/devices',
    );
  }

  Future<Map<String, dynamic>> _send(
    String method,
    String path, {
    Map<String, String>? query,
    Map<String, Object?>? body,
    required String route,
  }) {
    final request = http.Request(method, _uri(path, query))
      ..headers.addAll({
        'Accept': 'application/json',
        if (body != null) 'Content-Type': 'application/json',
      });
    if (body != null) request.body = jsonEncode(body);
    return _dispatch(request, route: route);
  }

  Future<Map<String, dynamic>> _dispatch(
    http.BaseRequest request, {
    required String route,
  }) async {
    if (!request.headers.containsKey('Accept')) {
      request.headers['Accept'] = 'application/json';
    }
    try {
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
    if (statusCode == 413) return 'The image is too large for the ledger.';
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
