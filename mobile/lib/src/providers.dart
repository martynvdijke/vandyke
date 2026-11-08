import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api/api_client.dart';
import 'api/api_exception.dart';
import 'api/models.dart';
import 'server_controller.dart';

final apiClientProvider = Provider<ApiClient>((ref) {
  final server = ref.watch(serverControllerProvider);
  final client = ApiClient(serverUrl: server.serverUrl);
  ref.onDispose(client.close);
  return client;
});

final statsProvider = FutureProvider.autoDispose<LedgerStats>((ref) async {
  return ref.watch(apiClientProvider).stats();
});

/// The person picker's source. Fails softly: the ledger screen derives a
/// fallback list from the entries it already has, so a missing endpoint or a
/// flaky network never blocks the feed (and is not retried in a loop).
final personsProvider = FutureProvider.autoDispose<List<PersonCount>>((
  ref,
) async {
  return ref.watch(apiClientProvider).persons();
}, retry: (_, _) => null);

/// The latest handful of slips for the active person filter, independent of
/// the feed's search term. Keyed by person so it never depends back on the
/// controller that invalidates it.
final recentEntriesProvider =
    FutureProvider.autoDispose.family<List<Entry>, String>((ref, person) async {
      final page = await ref
          .watch(apiClientProvider)
          .entries(person: person, page: 1, perPage: 5, sort: 'created_at');
      return page.items.take(5).toList();
    });

enum LedgerSort {
  newest('Newest first', 'created_at', 'desc'),
  alphabetical('A–Z', 'word', 'asc');

  const LedgerSort(this.label, this.sort, this.order);

  final String label;
  final String sort;
  final String order;

  LedgerSort get other =>
      this == LedgerSort.newest ? LedgerSort.alphabetical : LedgerSort.newest;
}

class LedgerState {
  const LedgerState({
    this.items = const [],
    this.total = 0,
    this.page = 1,
    this.pages = 0,
    this.query = '',
    this.person = '',
    this.sort = LedgerSort.newest,
    this.loading = false,
    this.loadingMore = false,
    this.error = '',
  });

  final List<Entry> items;
  final int total;
  final int page;
  final int pages;
  final String query;
  final String person;
  final LedgerSort sort;
  final bool loading;
  final bool loadingMore;
  final String error;

  bool get hasMore => page < pages;
  bool get isEmpty => !loading && items.isEmpty;
  bool get isFiltered => query.isNotEmpty || person.isNotEmpty;

  LedgerState copyWith({
    List<Entry>? items,
    int? total,
    int? page,
    int? pages,
    String? query,
    String? person,
    LedgerSort? sort,
    bool? loading,
    bool? loadingMore,
    String? error,
  }) {
    return LedgerState(
      items: items ?? this.items,
      total: total ?? this.total,
      page: page ?? this.page,
      pages: pages ?? this.pages,
      query: query ?? this.query,
      person: person ?? this.person,
      sort: sort ?? this.sort,
      loading: loading ?? this.loading,
      loadingMore: loadingMore ?? this.loadingMore,
      error: error ?? this.error,
    );
  }
}

final ledgerProvider = NotifierProvider<LedgerController, LedgerState>(
  LedgerController.new,
);

class LedgerController extends Notifier<LedgerState> {
  static const int _perPage = 25;

  int _requestId = 0;

  @override
  LedgerState build() {
    // A new API client (for example after changing server) rebuilds the feed.
    ref.watch(apiClientProvider);
    scheduleMicrotask(() => _loadFirst(reset: true));
    return const LedgerState(loading: true);
  }

  ApiClient get _api => ref.read(apiClientProvider);

  Future<void> refresh() async {
    ref.invalidate(recentEntriesProvider);
    ref.invalidate(personsProvider);
    await _loadFirst(reset: false);
    ref.invalidate(statsProvider);
  }

  Future<void> setQuery(String value) async {
    final query = value.trim();
    if (query == state.query) return;
    state = state.copyWith(query: query);
    await _loadFirst(reset: true);
  }

  /// Narrows the ledger to one attributed person; an empty value shows every
  /// entry again. Pagination always restarts with the new filter.
  Future<void> setPerson(String value) async {
    final person = value.trim();
    if (person.toLowerCase() == state.person.toLowerCase()) return;
    state = state.copyWith(person: person);
    await _loadFirst(reset: true);
  }

  Future<void> setSort(LedgerSort sort) async {
    if (sort == state.sort) return;
    state = state.copyWith(sort: sort);
    await _loadFirst(reset: true);
  }

  /// Drops both the search term and the person filter in one reload.
  Future<void> clearFilters() async {
    if (state.query.isEmpty && state.person.isEmpty) return;
    state = state.copyWith(query: '', person: '');
    await _loadFirst(reset: true);
  }

  Future<void> toggleSort() => setSort(state.sort.other);

  Future<void> _loadFirst({required bool reset}) async {
    final id = ++_requestId;
    final query = state.query;
    final person = state.person;
    final sort = state.sort;
    state = state.copyWith(
      loading: true,
      loadingMore: false,
      error: '',
      items: reset ? const [] : state.items,
      total: reset ? 0 : state.total,
      page: reset ? 1 : state.page,
    );
    try {
      final page = await _api.entries(
        query: query,
        person: person,
        sort: sort.sort,
        order: sort.order,
        page: 1,
        perPage: _perPage,
      );
      if (id != _requestId) return;
      state = state.copyWith(
        items: page.items,
        total: page.total,
        page: page.page,
        pages: page.pages,
        loading: false,
      );
    } on ApiException catch (error) {
      if (id != _requestId) return;
      state = state.copyWith(loading: false, error: error.friendlyMessage);
    } catch (_) {
      if (id != _requestId) return;
      state = state.copyWith(
        loading: false,
        error: 'Could not reach the ledger. Check your connection.',
      );
    }
  }

  Future<void> loadMore() async {
    if (state.loading || state.loadingMore || !state.hasMore) return;
    final id = _requestId;
    final query = state.query;
    final person = state.person;
    final sort = state.sort;
    final nextPage = state.page + 1;
    state = state.copyWith(loadingMore: true);
    try {
      final page = await _api.entries(
        query: query,
        person: person,
        sort: sort.sort,
        order: sort.order,
        page: nextPage,
        perPage: _perPage,
      );
      if (id != _requestId) return;
      final seen = state.items.map((entry) => entry.id).toSet();
      final merged = [
        ...state.items,
        ...page.items.where((entry) => !seen.contains(entry.id)),
      ];
      state = state.copyWith(
        items: merged,
        total: page.total,
        page: page.page,
        pages: page.pages,
        loadingMore: false,
      );
    } on ApiException catch (error) {
      if (id != _requestId) return;
      state = state.copyWith(loadingMore: false, error: error.friendlyMessage);
    } catch (_) {
      if (id != _requestId) return;
      state = state.copyWith(
        loadingMore: false,
        error: 'Could not load more entries.',
      );
    }
  }

  /// Inserts a freshly created entry at the top of the current feed and
  /// refreshes the counters, person picker, and recent strip.
  Future<void> acceptCreated(Entry entry) async {
    final matchesPerson =
        state.person.isEmpty ||
        entry.person.toLowerCase() == state.person.toLowerCase();
    if (state.query.isEmpty && matchesPerson) {
      state = state.copyWith(
        items: [entry, ...state.items.where((item) => item.id != entry.id)],
        total: state.total + 1,
      );
    }
    ref.invalidate(statsProvider);
    ref.invalidate(personsProvider);
    ref.invalidate(recentEntriesProvider);
  }
}
