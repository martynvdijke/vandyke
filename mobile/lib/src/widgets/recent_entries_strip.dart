import 'package:flutter/material.dart';

import '../api/models.dart';
import '../utils.dart';
import 'entry_detail_sheet.dart';

/// A compact reading of the newest slips, shown above the paginated feed.
///
/// Each line keeps the ledger's grammar: the misspelling struck through, the
/// correction it should have been, then who and when.
class RecentEntriesStrip extends StatelessWidget {
  const RecentEntriesStrip({super.key, required this.entries});

  final List<Entry> entries;

  @override
  Widget build(BuildContext context) {
    if (entries.isEmpty) return const SizedBox.shrink();
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Material(
      color: scheme.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(18),
        side: BorderSide(color: scheme.outlineVariant),
      ),
      clipBehavior: Clip.antiAlias,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 8, 6),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.only(right: 8, bottom: 2),
              child: Row(
                children: [
                  Text(
                    'LATEST SLIPS',
                    style: theme.textTheme.labelSmall?.copyWith(
                      letterSpacing: 1.7,
                      fontWeight: FontWeight.w700,
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                  const Spacer(),
                  Text(
                    'Newest first',
                    style: theme.textTheme.labelSmall?.copyWith(
                      fontSize: 9.5,
                      letterSpacing: 0.8,
                      fontWeight: FontWeight.w500,
                      color: scheme.onSurfaceVariant.withValues(alpha: .8),
                    ),
                  ),
                ],
              ),
            ),
            for (var i = 0; i < entries.length; i++) ...[
              if (i > 0)
                Divider(
                  height: 1,
                  color: scheme.outlineVariant.withValues(alpha: .55),
                ),
              _RecentRow(entry: entries[i]),
            ],
          ],
        ),
      ),
    );
  }
}

class _RecentRow extends StatelessWidget {
  const _RecentRow({required this.entry});

  final Entry entry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return InkWell(
      onTap: () => showEntryDetailSheet(context, entry),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(0, 9, 6, 9),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Row(
                    children: [
                      Flexible(
                        child: Text(
                          entry.word,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontFamily: 'Newsreader',
                            fontSize: 15.5,
                            height: 1.2,
                            fontWeight: FontWeight.w500,
                            color: scheme.onSurfaceVariant,
                            decoration: TextDecoration.lineThrough,
                            decorationColor: scheme.onSurfaceVariant.withValues(
                              alpha: .6,
                            ),
                          ),
                        ),
                      ),
                      Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 6),
                        child: Icon(
                          Icons.arrow_right_alt,
                          size: 16,
                          color: scheme.secondary,
                        ),
                      ),
                      Flexible(
                        flex: 2,
                        child: Text(
                          entry.meaning,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            fontSize: 13,
                            color: scheme.onSurface,
                          ),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 3),
                  Row(
                    children: [
                      if (entry.person.isNotEmpty) ...[
                        Flexible(child: _PersonTag(person: entry.person)),
                        const SizedBox(width: 7),
                      ],
                      Text(
                        formatShortDate(entry.createdAt),
                        style: theme.textTheme.labelSmall?.copyWith(
                          fontSize: 9.5,
                          letterSpacing: 0.6,
                          fontWeight: FontWeight.w600,
                          color: scheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            Icon(
              Icons.chevron_right,
              size: 17,
              color: scheme.outlineVariant,
            ),
          ],
        ),
      ),
    );
  }
}

class _PersonTag extends StatelessWidget {
  const _PersonTag({required this.person});

  final String person;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1.5),
      decoration: BoxDecoration(
        color: scheme.primaryContainer,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        person,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: theme.textTheme.labelSmall?.copyWith(
          fontSize: 9.5,
          letterSpacing: 0.2,
          fontWeight: FontWeight.w700,
          color: scheme.primary,
        ),
      ),
    );
  }
}
