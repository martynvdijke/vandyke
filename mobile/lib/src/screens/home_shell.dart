import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';
import '../widgets/common.dart';
import '../widgets/log_misspelling_sheet.dart';
import 'ledger_screen.dart';
import 'settings_screen.dart';
import 'stats_screen.dart';

/// The tab HomeShell shows. Kept outside the widget so a notification tap can
/// bring the ledger forward.
final homeTabProvider = NotifierProvider<HomeTabController, int>(
  HomeTabController.new,
);

class HomeTabController extends Notifier<int> {
  @override
  int build() => 0;

  void select(int index) {
    if (index < 0 || index > 2 || index == state) return;
    state = index;
  }
}

class HomeShell extends ConsumerWidget {
  const HomeShell({super.key});

  static const _screens = <Widget>[
    LedgerScreen(),
    StatsScreen(),
    SettingsScreen(),
  ];

  Future<void> _openLogSheet(BuildContext context, WidgetRef ref) async {
    final entry = await showLogMisspellingSheet(context);
    if (entry == null || !context.mounted) return;
    await ref.read(ledgerProvider.notifier).acceptCreated(entry);
    if (context.mounted) {
      showVandykeSnack(context, '“${entry.word}” added to the ledger.');
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final scheme = Theme.of(context).colorScheme;
    final index = ref.watch(homeTabProvider);
    return Scaffold(
      body: IndexedStack(index: index, children: _screens),
      floatingActionButton: index == 0
          ? FloatingActionButton.extended(
              onPressed: () => _openLogSheet(context, ref),
              backgroundColor: scheme.primary,
              foregroundColor: scheme.onPrimary,
              icon: const Icon(Icons.edit_note_outlined),
              label: const Text('Log a misspelling'),
            )
          : null,
      bottomNavigationBar: NavigationBar(
        selectedIndex: index,
        onDestinationSelected: ref.read(homeTabProvider.notifier).select,
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.menu_book_outlined),
            selectedIcon: Icon(Icons.menu_book),
            label: 'Ledger',
          ),
          NavigationDestination(
            icon: Icon(Icons.insights_outlined),
            selectedIcon: Icon(Icons.insights),
            label: 'Stats',
          ),
          NavigationDestination(
            icon: Icon(Icons.tune_outlined),
            selectedIcon: Icon(Icons.tune),
            label: 'Settings',
          ),
        ],
      ),
    );
  }
}
