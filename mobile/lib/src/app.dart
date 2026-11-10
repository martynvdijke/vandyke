import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'deep_links.dart';
import 'screens/home_shell.dart';
import 'screens/setup_screen.dart';
import 'server_controller.dart';
import 'theme/theme_controller.dart';
import 'theme/vandyke_theme.dart';
import 'widgets/common.dart';
import 'widgets/entry_detail_sheet.dart';

class VandykeApp extends ConsumerStatefulWidget {
  const VandykeApp({super.key});

  @override
  ConsumerState<VandykeApp> createState() => _VandykeAppState();
}

class _VandykeAppState extends ConsumerState<VandykeApp> {
  final _navigatorKey = GlobalKey<NavigatorState>();
  StreamSubscription<Uri>? _linkSubscription;

  /// A cold-start link that arrived before the server address was loaded; it
  /// is routed as soon as the app reaches the ready state.
  Uri? _pendingLink;

  @override
  void initState() {
    super.initState();
    final source = ref.read(entryLinkSourceProvider);
    _linkSubscription = source.links.listen(_handleLink);
    unawaited(_readInitialLink(source));
  }

  Future<void> _readInitialLink(EntryLinkSource source) async {
    Uri? initial;
    try {
      initial = await source.initialLink();
    } catch (_) {
      initial = null;
    }
    if (initial == null || !mounted) return;
    _pendingLink = initial;
    _routePending();
  }

  @override
  void dispose() {
    _linkSubscription?.cancel();
    super.dispose();
  }

  void _handleLink(Uri uri) {
    _pendingLink = uri;
    _routePending();
  }

  /// Routes a held link once the ledger address is known; otherwise waits for
  /// the server controller to reach `ready`.
  void _routePending() {
    final pending = _pendingLink;
    if (pending == null) return;
    if (ref.read(serverControllerProvider).status != ServerStatus.ready) {
      return;
    }
    _pendingLink = null;
    final link = parseEntryLink(pending);
    if (link == null) return;
    final serverUrl = ref.read(serverControllerProvider).serverUrl;
    if (!linkBelongsToLedger(pending, serverUrl)) {
      _notify('That link belongs to a different ledger.');
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) => _openEntry(link.id));
  }

  Future<void> _openEntry(int id) async {
    final context = _navigatorKey.currentContext;
    if (context == null) return;
    await showEntryDetailSheetById(context, id);
  }

  void _notify(String message) {
    final context = _navigatorKey.currentContext;
    if (context == null) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (context.mounted) showVandykeSnack(context, message, error: true);
    });
  }

  @override
  Widget build(BuildContext context) {
    final server = ref.watch(serverControllerProvider);
    final mode = ref.watch(themeControllerProvider);
    ref.listen(serverControllerProvider, (previous, next) {
      if (next.status == ServerStatus.ready) _routePending();
    });
    return MaterialApp(
      title: 'The vanDyke Ledger',
      debugShowCheckedModeBanner: false,
      navigatorKey: _navigatorKey,
      theme: VandykeTheme.light(),
      darkTheme: VandykeTheme.dark(),
      themeMode: mode,
      home: switch (server.status) {
        ServerStatus.loading => const _SplashScreen(),
        ServerStatus.unconfigured => const SetupScreen(),
        ServerStatus.ready => const HomeShell(),
      },
    );
  }
}

class _SplashScreen extends StatelessWidget {
  const _SplashScreen();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const VandykeLogo(size: 72),
            const SizedBox(height: 20),
            Text('The vanDyke Ledger', style: theme.textTheme.headlineSmall),
            const SizedBox(height: 8),
            Text(
              'Opening the corrections file…',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 24),
            SizedBox(
              width: 20,
              height: 20,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: scheme.primary,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
