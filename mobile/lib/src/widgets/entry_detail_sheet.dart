import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_exception.dart';
import '../api/models.dart';
import '../providers.dart';
import '../share.dart';
import '../utils.dart';
import 'common.dart';

/// Opens the shared entry dossier for an entry already held in memory.
///
/// Passing the loaded entry means opening a card or a strip row never repeats
/// the request the feed just made.
Future<void> showEntryDetailSheet(BuildContext context, Entry entry) {
  return _open(context, EntryDetailSheet(entry: entry));
}

/// Opens the dossier for an id that is not necessarily loaded, resolving it
/// with `GET /api/v1/entries/{id}` — the path a deep link or permalink takes.
Future<void> showEntryDetailSheetById(BuildContext context, int entryId) {
  return _open(context, EntryDetailSheet(entryId: entryId));
}

Future<void> _open(BuildContext context, EntryDetailSheet sheet) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => sheet,
  );
}

class EntryDetailSheet extends ConsumerStatefulWidget {
  const EntryDetailSheet({super.key, this.entry, this.entryId})
    : assert(
        entry != null || entryId != null,
        'Provide an entry or an entry id to open the dossier.',
      );

  final Entry? entry;
  final int? entryId;

  @override
  ConsumerState<EntryDetailSheet> createState() => _EntryDetailSheetState();
}

class _EntryDetailSheetState extends ConsumerState<EntryDetailSheet> {
  Entry? _entry;
  String _error = '';
  bool _loading = false;
  bool _notFound = false;

  int? get _entryId => widget.entry?.id ?? widget.entryId;

  @override
  void initState() {
    super.initState();
    _entry = widget.entry;
    if (_entry == null) _load();
  }

  Future<void> _load() async {
    final id = widget.entryId;
    if (id == null) return;
    setState(() {
      _loading = true;
      _error = '';
      _notFound = false;
    });
    try {
      final entry = await ref.read(apiClientProvider).entryById(id);
      if (!mounted) return;
      setState(() {
        _entry = entry;
        _loading = false;
      });
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _notFound = error.isNotFound;
        _error = error.friendlyMessage;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = 'Could not reach the ledger. Check your connection.';
      });
    }
  }

  Future<void> _share() async {
    final id = _entryId;
    if (id == null) return;
    final serverUrl = ref.read(apiClientProvider).baseUrl;
    await shareEntryPermalink(
      serverUrl: serverUrl,
      entryId: id,
      sharer: ref.read(urlSharerProvider),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final entry = _entry;
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(24, 10, 24, 32),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Center(
            child: Container(
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: scheme.outlineVariant,
                borderRadius: BorderRadius.circular(999),
              ),
            ),
          ),
          const SizedBox(height: 22),
          if (entry == null && _loading)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 28),
              child: Center(
                child: SizedBox(
                  width: 22,
                  height: 22,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
              ),
            )
          else if (entry == null)
            ErrorPanel(
              message: _error,
              onRetry: _notFound ? null : _load,
            )
          else
            ..._body(context, theme, entry),
        ],
      ),
    );
  }

  List<Widget> _body(BuildContext context, ThemeData theme, Entry entry) {
    final scheme = theme.colorScheme;
    final baseUrl = ref.watch(apiClientProvider).baseUrl;
    return [
      Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: _EntryEyebrow('Ledger entry · no. ${entry.id}'),
          ),
          IconButton(
            key: const ValueKey('entry-share'),
            tooltip: 'Share this entry',
            onPressed: _share,
            visualDensity: VisualDensity.compact,
            icon: const Icon(Icons.ios_share, size: 20),
            color: scheme.primary,
          ),
        ],
      ),
      const SizedBox(height: 6),
      Text(
        entry.word,
        style: TextStyle(
          fontFamily: 'Newsreader',
          fontSize: 30,
          height: 1.1,
          letterSpacing: -0.6,
          fontWeight: FontWeight.w500,
          color: scheme.onSurfaceVariant,
          decoration: TextDecoration.lineThrough,
          decorationColor: scheme.onSurfaceVariant.withValues(alpha: .6),
          decorationThickness: 1.4,
        ),
      ),
      const SizedBox(height: 10),
      Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 3),
            child: Icon(
              Icons.arrow_right_alt,
              size: 22,
              color: scheme.secondary,
            ),
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              entry.meaning,
              style: theme.textTheme.headlineSmall?.copyWith(
                fontFamily: 'Newsreader',
                fontSize: 21,
                height: 1.3,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
        ],
      ),
      const SizedBox(height: 18),
      Divider(color: scheme.outlineVariant),
      const SizedBox(height: 14),
      if (entry.person.isNotEmpty) ...[
        _DetailRow(
          label: 'Attributed to',
          child: Text(entry.person, style: theme.textTheme.titleMedium),
        ),
        const SizedBox(height: 12),
      ],
      _DetailRow(
        label: 'Logged',
        child: Text(
          formatLedgerDate(entry.createdAt),
          style: theme.textTheme.titleMedium,
        ),
      ),
      if (entry.hasPhoto) ...[
        const SizedBox(height: 18),
        ClipRRect(
          borderRadius: BorderRadius.circular(14),
          child: Image.network(
            entry.photoFor(baseUrl, size: 'display'),
            width: double.infinity,
            fit: BoxFit.cover,
            semanticLabel: 'Photo evidence for ${entry.word}',
            errorBuilder: (_, _, _) => Container(
              height: 160,
              color: scheme.surfaceContainerHigh,
              alignment: Alignment.center,
              child: Icon(
                Icons.image_not_supported_outlined,
                color: scheme.onSurfaceVariant,
              ),
            ),
            loadingBuilder: (context, child, progress) {
              if (progress == null) return child;
              return Container(
                height: 160,
                color: scheme.surfaceContainerHigh,
                alignment: Alignment.center,
                child: const SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
              );
            },
          ),
        ),
      ],
    ];
  }
}

/// A small uppercase label; kept local so the sheet does not depend on the
/// common widgets' text sizing.
class _EntryEyebrow extends StatelessWidget {
  const _EntryEyebrow(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Text(
      text.toUpperCase(),
      style: Theme.of(context).textTheme.labelSmall?.copyWith(
        letterSpacing: 1.7,
        fontWeight: FontWeight.w700,
        color: Theme.of(context).colorScheme.onSurfaceVariant,
      ),
    );
  }
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({required this.label, required this.child});

  final String label;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.baseline,
      textBaseline: TextBaseline.alphabetic,
      children: [
        SizedBox(
          width: 120,
          child: Text(
            label,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ),
        Expanded(child: child),
      ],
    );
  }
}
