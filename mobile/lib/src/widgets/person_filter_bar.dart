import 'package:flutter/material.dart';

import '../api/models.dart';

/// A quietly ruled row of person filters: "Everyone" plus one pill per name.
///
/// The bar always shows at least "Everyone" so the layout never jumps while
/// the optional `/persons` request is in flight.
class PersonFilterBar extends StatelessWidget {
  const PersonFilterBar({
    super.key,
    required this.persons,
    required this.selected,
    required this.onSelected,
  });

  final List<PersonCount> persons;
  final String selected;
  final ValueChanged<String> onSelected;

  @override
  Widget build(BuildContext context) {
    final selectedName = selected.toLowerCase();
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      physics: const BouncingScrollPhysics(),
      child: Row(
        children: [
          _PersonPill(
            label: 'Everyone',
            selected: selected.isEmpty,
            onTap: () => onSelected(''),
          ),
          for (final person in persons) ...[
            const SizedBox(width: 8),
            _PersonPill(
              label: person.person,
              count: person.count,
              selected: selectedName == person.person.toLowerCase(),
              onTap: () => onSelected(person.person),
            ),
          ],
        ],
      ),
    );
  }
}

class _PersonPill extends StatelessWidget {
  const _PersonPill({
    required this.label,
    required this.selected,
    required this.onTap,
    this.count = 0,
  });

  final String label;
  final int count;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final foreground = selected ? scheme.onPrimary : scheme.onSurface;
    final muted = selected
        ? scheme.onPrimary.withValues(alpha: .68)
        : scheme.onSurfaceVariant;
    return Semantics(
      button: true,
      selected: selected,
      label: count > 0 ? '$label, $count entries' : label,
      child: Material(
        color: selected ? scheme.primary : scheme.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(999),
          side: BorderSide(
            color: selected ? scheme.primary : scheme.outlineVariant,
          ),
        ),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(999),
          child: Padding(
            padding: EdgeInsets.fromLTRB(selected ? 9 : 13, 7, 13, 7),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (selected) ...[
                  Icon(Icons.check, size: 13, color: foreground),
                  const SizedBox(width: 5),
                ],
                ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 170),
                  child: Text(
                    label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.labelLarge?.copyWith(
                      fontSize: 12.5,
                      fontWeight: selected ? FontWeight.w700 : FontWeight.w600,
                      letterSpacing: 0.1,
                      color: foreground,
                    ),
                  ),
                ),
                if (count > 0) ...[
                  const SizedBox(width: 6),
                  Container(
                    width: 3,
                    height: 3,
                    decoration: BoxDecoration(
                      color: muted,
                      shape: BoxShape.circle,
                    ),
                  ),
                  const SizedBox(width: 6),
                  Text(
                    '$count',
                    style: theme.textTheme.labelSmall?.copyWith(
                      fontSize: 10,
                      letterSpacing: 0.3,
                      fontWeight: FontWeight.w700,
                      color: muted,
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Rebuilds a person list from the entries already on screen, used when
/// `/persons` is unavailable. Names keep their first-seen spelling and are
/// ordered case-insensitively, matching the server.
List<PersonCount> derivePersons(List<Entry> entries) {
  final counts = <String, PersonCount>{};
  for (final entry in entries) {
    final name = entry.person.trim();
    if (name.isEmpty) continue;
    final key = name.toLowerCase();
    final existing = counts[key];
    counts[key] = PersonCount(
      person: existing?.person ?? name,
      count: (existing?.count ?? 0) + 1,
    );
  }
  final persons = counts.values.toList()
    ..sort(
      (a, b) => a.person.toLowerCase().compareTo(b.person.toLowerCase()),
    );
  return persons;
}
