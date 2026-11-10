import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:share_plus/share_plus.dart';

import 'deep_links.dart';

/// Opens the platform share sheet for a URL. Injectable so widget tests can
/// capture the shared link without a platform channel.
typedef UrlSharer =
    Future<void> Function(String url, {String subject});

/// Shares through `share_plus`; a dismissed or unavailable sheet is not an
/// error the user needs to see.
Future<void> platformUrlShare(
  String url, {
  String subject = 'vanDyke ledger entry',
}) async {
  try {
    await SharePlus.instance.share(
      ShareParams(uri: Uri.parse(url), subject: subject),
    );
  } catch (_) {
    // The user dismissed the sheet, or no handler exists. Neither is fatal.
  }
}

final urlSharerProvider = Provider<UrlSharer>((ref) => platformUrlShare);

/// Shares the canonical `{server}/e/{id}` permalink for an entry.
Future<void> shareEntryPermalink({
  required String serverUrl,
  required int entryId,
  required UrlSharer sharer,
}) {
  return sharer(
    entryPermalink(serverUrl, entryId),
    subject: 'Ledger entry no. $entryId',
  );
}
