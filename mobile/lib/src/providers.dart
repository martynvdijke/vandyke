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
  final LedgerSort sort;
  final bool loading;
  final bool loadingMore;
  final String error;

  bool get hasMore => page < pages;
  bool get isEmpty => !loading && items.isEmpty;

  LedgerState copyWith({
    List<Entry>? items,
    int? total,
    int? page,
    int? pages,
    String? query,
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
    await _loadFirst(reset: false);
    ref.invalidate(statsProvider);
  }

  Future<void> setQuery(String value) async {
    final query = value.trim();
    if (query == state.query) return;
    state = state.copyWith(query: query);
    await _loadFirst(reset: true);
  }

  Future<void> setSort(LedgerSort sort) async {
    if (sort == state.sort) return;
    state = state.copyWith(sort: sort);
    await _loadFirst(reset: true);
  }

  Future<void> toggleSort() => setSort(state.sort.other);

  Future<void> _loadFirst({required bool reset}) async {
    final id = ++_requestId;
    final query = state.query;
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
    final sort = state.sort;
    final nextPage = state.page + 1;
    state = state.copyWith(loadingMore: true);
    try {
      final page = await _api.entries(
        query: query,
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
  /// refreshes the stats counters.
  Future<void> acceptCreated(Entry entry) async {
    if (state.query.isEmpty) {
      state = state.copyWith(
        items: [entry, ...state.items.where((item) => item.id != entry.id)],
        total: state.total + 1,
      );
    }
    ref.invalidate(statsProvider);
  }
}
