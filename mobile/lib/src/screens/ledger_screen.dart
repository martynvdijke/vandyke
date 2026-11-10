import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/models.dart';
import '../providers.dart';
import '../server_controller.dart';
import '../share.dart';
import '../utils.dart';
import '../widgets/common.dart';
import '../widgets/entry_card.dart';
import '../widgets/log_misspelling_sheet.dart';
import '../widgets/person_filter_bar.dart';
import '../widgets/recent_entries_strip.dart';

class LedgerScreen extends ConsumerStatefulWidget {
  const LedgerScreen({super.key});

  @override
  ConsumerState<LedgerScreen> createState() => _LedgerScreenState();
}

class _LedgerScreenState extends ConsumerState<LedgerScreen> {
  final _search = TextEditingController();
  final _scroll = ScrollController();
  Timer? _debounce;
  bool _showSearchHint = false;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _search.dispose();
    _scroll
      ..removeListener(_onScroll)
      ..dispose();
    super.dispose();
  }

  void _onScroll() {
    if (!_scroll.hasClients) return;
    final remaining =
        _scroll.position.maxScrollExtent - _scroll.position.pixels;
    if (remaining < 420) {
      ref.read(ledgerProvider.notifier).loadMore();
    }
  }

  void _onQueryChanged(String value) {
    setState(() => _showSearchHint = value.trim().isNotEmpty);
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 350), () {
      ref.read(ledgerProvider.notifier).setQuery(value);
    });
  }

  void _clearQuery() {
    _debounce?.cancel();
    _search.clear();
    setState(() => _showSearchHint = false);
    ref.read(ledgerProvider.notifier).setQuery('');
  }

  Future<void> _selectPerson(String person) async {
    if (_scroll.hasClients) _scroll.jumpTo(0);
    await ref.read(ledgerProvider.notifier).setPerson(person);
  }

  Future<void> _clearFilters() async {
    _debounce?.cancel();
    _search.clear();
    setState(() => _showSearchHint = false);
    await ref.read(ledgerProvider.notifier).clearFilters();
  }

  Future<void> _refresh() async {
    await ref.read(ledgerProvider.notifier).refresh();
  }

  Future<void> _openLogSheet() async {
    final entry = await showLogMisspellingSheet(context);
    if (entry == null || !mounted) return;
    await ref.read(ledgerProvider.notifier).acceptCreated(entry);
    if (!mounted) return;
    final serverUrl = ref.read(serverControllerProvider).serverUrl;
    showVandykeSnack(
      context,
      '“${entry.word}” added to the ledger.',
      action: SnackBarAction(
        label: 'Share',
        onPressed: () => shareEntryPermalink(
          serverUrl: serverUrl,
          entryId: entry.id,
          sharer: ref.read(urlSharerProvider),
        ),
      ),
    );
  }

  String _subtitle(LedgerState state) {
    final person = state.person;
    final query = state.query;
    if (person.isNotEmpty && query.isNotEmpty) {
      return '“$query” as filed by $person.';
    }
    if (person.isNotEmpty) {
      return 'Every slip attributed to $person.';
    }
    if (query.isNotEmpty) return 'Results for “$query”.';
    return 'Every creative spelling of the name, filed for the record.';
  }

  String _countLabel(LedgerState state) {
    if (state.loading) return 'Searching…';
    if (state.person.isEmpty) {
      return '${pluralize(state.total, 'entry', 'entries')} on file';
    }
    return '${pluralize(state.total, 'slip', 'slips')} by ${state.person}';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = ref.watch(ledgerProvider);
    final stats = ref.watch(statsProvider);
    final personsAsync = ref.watch(personsProvider);
    final persons = _personsFor(state, personsAsync.value);
    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(22, 18, 22, 0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Eyebrow('The LANCo corrections ledger'),
                  const SizedBox(height: 4),
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: Text(
                          'The Ledger',
                          style: theme.textTheme.displaySmall,
                        ),
                      ),
                      IconButton(
                        tooltip: 'Log a new misspelling',
                        onPressed: _openLogSheet,
                        icon: const Icon(Icons.add_circle_outline),
                        color: theme.colorScheme.primary,
                      ),
                    ],
                  ),
                  const SizedBox(height: 2),
                  Text(
                    _subtitle(state),
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 16),
                  _StatsStrip(
                    total: stats.value?.total ?? state.total,
                    unique: stats.value?.uniqueWords ?? 0,
                    last30: stats.value?.entriesLast30Days ?? 0,
                    loading: stats.isLoading,
                  ),
                  const SizedBox(height: 14),
                  TextField(
                    controller: _search,
                    onChanged: _onQueryChanged,
                    textInputAction: TextInputAction.search,
                    decoration: InputDecoration(
                      hintText: 'Search the misspellings',
                      prefixIcon: const Icon(Icons.search, size: 20),
                      suffixIcon: _showSearchHint
                          ? IconButton(
                              tooltip: 'Clear search',
                              onPressed: _clearQuery,
                              icon: const Icon(Icons.close, size: 18),
                            )
                          : null,
                    ),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          _countLabel(state),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.labelSmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                            letterSpacing: 0.6,
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      _SortChip(
                        label: state.sort.label,
                        icon: state.sort == LedgerSort.newest
                            ? Icons.south
                            : Icons.sort_by_alpha,
                        onTap: () =>
                            ref.read(ledgerProvider.notifier).toggleSort(),
                      ),
                    ],
                  ),
                  const SizedBox(height: 10),
                  if (persons.isNotEmpty || state.person.isNotEmpty)
                    PersonFilterBar(
                      persons: persons,
                      selected: state.person,
                      onSelected: _selectPerson,
                    ),
                  const SizedBox(height: 6),
                ],
              ),
            ),
            Expanded(child: _buildBody(state, theme)),
          ],
        ),
      ),
    );
  }

  /// The server list, or a list derived from what is already on screen when
  /// `/persons` is missing or still loading.
  List<PersonCount> _personsFor(
    LedgerState state,
    List<PersonCount>? fromServer,
  ) {
    final persons = List<PersonCount>.of(
      fromServer ?? derivePersons(state.items),
    );
    final selected = state.person.toLowerCase();
    if (selected.isNotEmpty &&
        !persons.any((person) => person.person.toLowerCase() == selected)) {
      persons.insert(0, PersonCount(person: state.person, count: state.total));
    }
    persons.sort(
      (a, b) => a.person.toLowerCase().compareTo(b.person.toLowerCase()),
    );
    return persons;
  }

  Widget _buildBody(LedgerState state, ThemeData theme) {
    if (state.loading && state.items.isEmpty) {
      return const Center(
        child: SizedBox(
          width: 22,
          height: 22,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }
    if (state.error.isNotEmpty && state.items.isEmpty) {
      return ListView(
        padding: const EdgeInsets.fromLTRB(22, 8, 22, 32),
        children: [
          ErrorPanel(
            message: state.error,
            onRetry: () => ref.read(ledgerProvider.notifier).refresh(),
          ),
        ],
      );
    }
    if (state.isEmpty) {
      return ListView(
        padding: const EdgeInsets.fromLTRB(22, 8, 22, 32),
        children: [_buildEmptyState(state)],
      );
    }

    final recent =
        ref.watch(recentEntriesProvider(state.person)).value ?? const <Entry>[];
    final showRecent = recent.isNotEmpty && state.query.isEmpty;
    final offset = showRecent ? 1 : 0;

    return Column(
      children: [
        if (state.error.isNotEmpty)
          Padding(
            padding: const EdgeInsets.fromLTRB(22, 0, 22, 10),
            child: ErrorPanel(
              message: state.error,
              onRetry: () => ref.read(ledgerProvider.notifier).refresh(),
            ),
          ),
        Expanded(
          child: RefreshIndicator(
            onRefresh: _refresh,
            child: ListView.separated(
              controller: _scroll,
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(22, 4, 22, 96),
              itemCount: offset + state.items.length + (state.hasMore ? 1 : 0),
              separatorBuilder: (_, _) => const SizedBox(height: 10),
              itemBuilder: (context, index) {
                if (showRecent && index == 0) {
                  return RecentEntriesStrip(entries: recent);
                }
                final itemIndex = index - offset;
                if (itemIndex >= state.items.length) {
                  return Padding(
                    padding: const EdgeInsets.symmetric(vertical: 18),
                    child: Center(
                      child: state.loadingMore
                          ? const SizedBox(
                              width: 20,
                              height: 20,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : TextButton(
                              onPressed: () =>
                                  ref.read(ledgerProvider.notifier).loadMore(),
                              child: const Text('Load more'),
                            ),
                    ),
                  );
                }
                return EntryCard(entry: state.items[itemIndex]);
              },
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildEmptyState(LedgerState state) {
    final person = state.person;
    final query = state.query;
    if (person.isNotEmpty && query.isNotEmpty) {
      return EmptyState(
        title: 'No matches for “$query”',
        message: 'Nothing filed by $person matches that search.',
        icon: Icons.search_off,
        actionLabel: 'Clear filters',
        onAction: _clearFilters,
      );
    }
    if (person.isNotEmpty) {
      return EmptyState(
        title: 'Nothing under that name',
        message: 'No slips recorded by $person yet.',
        icon: Icons.person_search_outlined,
        actionLabel: 'Show everyone',
        onAction: () => _selectPerson(''),
      );
    }
    if (query.isNotEmpty) {
      return EmptyState(
        title: 'No misspellings match',
        message:
            'Try a shorter fragment, or clear the search to see every entry.',
        icon: Icons.search_off,
        actionLabel: 'Clear',
        onAction: _clearQuery,
      );
    }
    return EmptyState(
      title: 'The ledger is blank',
      message: 'Log the first creative spelling of the name.',
      icon: Icons.menu_book_outlined,
      actionLabel: 'Log a misspelling',
      onAction: _openLogSheet,
    );
  }
}

class _StatsStrip extends StatelessWidget {
  const _StatsStrip({
    required this.total,
    required this.unique,
    required this.last30,
    required this.loading,
  });

  final int total;
  final int unique;
  final int last30;
  final bool loading;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    String figure(int value) =>
        loading && value == 0 ? '—' : formatCount(value);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
      decoration: BoxDecoration(
        color: scheme.surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Row(
        children: [
          Expanded(
            child: StatFigure(
              value: figure(total),
              label: 'Total',
              emphasized: true,
            ),
          ),
          _Divider(color: scheme.outlineVariant),
          Expanded(
            child: StatFigure(value: figure(unique), label: 'Unique'),
          ),
          _Divider(color: scheme.outlineVariant),
          Expanded(
            child: StatFigure(value: figure(last30), label: 'Last 30 days'),
          ),
        ],
      ),
    );
  }
}

class _Divider extends StatelessWidget {
  const _Divider({required this.color});

  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 1,
      height: 34,
      margin: const EdgeInsets.symmetric(horizontal: 12),
      color: color.withValues(alpha: .7),
    );
  }
}

class _SortChip extends StatelessWidget {
  const _SortChip({
    required this.label,
    required this.icon,
    required this.onTap,
  });

  final String label;
  final IconData icon;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: scheme.surfaceContainerHigh,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(999),
        side: BorderSide(color: scheme.outlineVariant),
      ),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(999),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(icon, size: 14, color: scheme.primary),
              const SizedBox(width: 6),
              Text(
                label,
                style: Theme.of(context).textTheme.labelSmall
                    ?.copyWith(color: scheme.primary, letterSpacing: 0.4),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
