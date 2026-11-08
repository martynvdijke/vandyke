import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/models.dart';
import '../providers.dart';
import '../utils.dart';

/// Opens the shared entry dossier used by both the feed cards and the
/// "Latest slips" strip.
Future<void> showEntryDetailSheet(BuildContext context, Entry entry) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => EntryDetailSheet(entry: entry),
  );
}

class EntryDetailSheet extends ConsumerWidget {
  const EntryDetailSheet({super.key, required this.entry});

  final Entry entry;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final baseUrl = ref.watch(apiClientProvider).baseUrl;
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
          _EntryEyebrow('Ledger entry · no. ${entry.id}'),
          const SizedBox(height: 10),
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
              child: Text(
                entry.person,
                style: theme.textTheme.titleMedium,
              ),
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
        ],
      ),
    );
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
