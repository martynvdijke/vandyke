import 'package:app_links/app_links.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api/api_client.dart';

/// A parsed `/e/{id}` permalink, in either its `https` or `vandyke` form.
class EntryLink {
  const EntryLink({required this.id, required this.uri});

  final int id;
  final Uri uri;

  @override
  String toString() => 'EntryLink($id, $uri)';
}

/// Builds the canonical absolute permalink for an entry.
String entryPermalink(String serverUrl, int id) {
  return '${ApiClient.normalizeServerUrl(serverUrl)}/e/$id';
}

/// Parses `https://<host>/e/<id>` and `vandyke://e/<id>` (also
/// `vandyke:///e/<id>`). Anything else, including a non-numeric id, is not an
/// entry link.
EntryLink? parseEntryLink(Uri uri) {
  final segments = uri.pathSegments
      .map((segment) => segment.trim())
      .where((segment) => segment.isNotEmpty)
      .toList();
  final scheme = uri.scheme.toLowerCase();

  if (scheme == 'http' || scheme == 'https') {
    if (segments.length != 2 || segments.first != 'e') return null;
    return _entryLink(segments[1], uri);
  }

  if (scheme == 'vandyke') {
    final host = uri.host.toLowerCase();
    if (host == 'e' && segments.length == 1) {
      return _entryLink(segments.first, uri);
    }
    if (host.isEmpty && segments.length == 2 && segments.first == 'e') {
      return _entryLink(segments[1], uri);
    }
  }

  return null;
}

EntryLink? _entryLink(String rawId, Uri uri) {
  final id = int.tryParse(rawId);
  if (id == null || id <= 0) return null;
  return EntryLink(id: id, uri: uri);
}

/// Whether a link may be resolved against the configured ledger.
///
/// The custom scheme can only be produced for this app, so it is accepted as
/// belonging here; an `https` link must name the same host and port as the
/// configured server, otherwise the app refuses to send the session there.
bool linkBelongsToLedger(Uri uri, String serverUrl) {
  final scheme = uri.scheme.toLowerCase();
  if (scheme == 'vandyke') return true;
  if (scheme != 'http' && scheme != 'https') return false;

  final server = Uri.tryParse(ApiClient.normalizeServerUrl(serverUrl));
  if (server == null || server.host.isEmpty) return false;
  if (uri.host.toLowerCase() != server.host.toLowerCase()) return false;
  return uri.port == server.port;
}

/// Where incoming permalinks come from; injectable so the app can be tested
/// without the platform channel.
abstract class EntryLinkSource {
  /// The link that launched the app, if any (cold start).
  Future<Uri?> initialLink();

  /// Links delivered while the app is running (warm start).
  Stream<Uri> get links;
}

class PlatformEntryLinkSource implements EntryLinkSource {
  PlatformEntryLinkSource({AppLinks? links}) : _links = links ?? AppLinks();

  final AppLinks _links;

  @override
  Future<Uri?> initialLink() => _links.getInitialLink();

  @override
  Stream<Uri> get links => _links.uriLinkStream;
}

final entryLinkSourceProvider = Provider<EntryLinkSource>(
  (ref) => PlatformEntryLinkSource(),
);
