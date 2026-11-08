import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/models.dart';
import '../providers.dart';
import '../utils.dart';

class EntryCard extends ConsumerWidget {
  const EntryCard({super.key, required this.entry});

  final Entry entry;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final baseUrl = ref.watch(apiClientProvider).baseUrl;
    return Container(
      padding: const EdgeInsets.fromLTRB(18, 16, 18, 16),
      decoration: BoxDecoration(
        color: scheme.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  entry.word,
                  style: theme.textTheme.headlineSmall?.copyWith(
                    fontFamily: 'Newsreader',
                    fontSize: 24,
                    letterSpacing: -0.4,
                  ),
                ),
                const SizedBox(height: 5),
                Text(
                  entry.meaning,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    if (entry.person.isNotEmpty)
                      _Meta(icon: Icons.person_outline, label: entry.person),
                    Text(
                      formatLedgerDate(entry.createdAt),
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                        letterSpacing: 0.6,
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
          if (entry.hasPhoto) ...[
            const SizedBox(width: 14),
            ClipRRect(
              borderRadius: BorderRadius.circular(10),
              child: SizedBox(
                width: 62,
                height: 62,
                child: Image.network(
                  entry.photoFor(baseUrl),
                  fit: BoxFit.cover,
                  semanticLabel: 'Photo evidence for ${entry.word}',
                  errorBuilder: (_, _, _) => _PhotoFallback(scheme: scheme),
                  loadingBuilder: (context, child, progress) {
                    if (progress == null) return child;
                    return _PhotoFallback(scheme: scheme);
                  },
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _Meta extends StatelessWidget {
  const _Meta({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 13, color: scheme.onSurfaceVariant),
        const SizedBox(width: 4),
        ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 160),
          child: Text(
            label,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: Theme.of(context).textTheme.labelSmall
                ?.copyWith(color: scheme.onSurfaceVariant, letterSpacing: 0.4),
          ),
        ),
      ],
    );
  }
}

class _PhotoFallback extends StatelessWidget {
  const _PhotoFallback({required this.scheme});

  final ColorScheme scheme;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: scheme.surfaceContainerHigh,
      alignment: Alignment.center,
      child: Icon(
        Icons.image_not_supported_outlined,
        size: 22,
        color: scheme.onSurfaceVariant,
      ),
    );
  }
}
