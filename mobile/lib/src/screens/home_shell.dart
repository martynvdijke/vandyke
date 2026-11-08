import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';
import '../widgets/common.dart';
import '../widgets/log_misspelling_sheet.dart';
import 'ledger_screen.dart';
import 'settings_screen.dart';
import 'stats_screen.dart';

class HomeShell extends ConsumerStatefulWidget {
  const HomeShell({super.key});

  @override
  ConsumerState<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends ConsumerState<HomeShell> {
  int _index = 0;

  static const _screens = <Widget>[
    LedgerScreen(),
    StatsScreen(),
    SettingsScreen(),
  ];

  Future<void> _openLogSheet() async {
    final entry = await showLogMisspellingSheet(context);
    if (entry == null || !mounted) return;
    await ref.read(ledgerProvider.notifier).acceptCreated(entry);
    if (mounted) {
      showVandykeSnack(context, '“${entry.word}” added to the ledger.');
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      body: IndexedStack(index: _index, children: _screens),
      floatingActionButton: _index == 0
          ? FloatingActionButton.extended(
              onPressed: _openLogSheet,
              backgroundColor: scheme.primary,
              foregroundColor: scheme.onPrimary,
              icon: const Icon(Icons.edit_note_outlined),
              label: const Text('Log a misspelling'),
            )
          : null,
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (index) => setState(() => _index = index),
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
