import 'dart:async';

import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'deep_links.dart';
import 'notifications/notification_providers.dart';
import 'notifications/push_service.dart';
import 'providers.dart';
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

  /// The service whose notification streams are currently wired up.
  PushService? _pushService;

  /// A service currently being started, so two calls never attach twice.
  PushService? _pushStarting;
  StreamSubscription<RemoteMessage>? _foregroundSubscription;
  StreamSubscription<RemoteMessage>? _openedSubscription;

  /// A cold-start link that arrived before the server address was loaded; it
  /// is routed as soon as the app reaches the ready state.
  Uri? _pendingLink;

  @override
  void initState() {
    super.initState();
    final source = ref.read(entryLinkSourceProvider);
    _linkSubscription = source.links.listen(_handleLink);
    unawaited(_readInitialLink(source));
    unawaited(_startPush());
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
    _foregroundSubscription?.cancel();
    _openedSubscription?.cancel();
    super.dispose();
  }

  /// Connects to the notification streams of the current push service.
  ///
  /// Firebase is only started when the device already had notifications on, so
  /// an unconfigured server leaves the streams silent and never blocks startup.
  Future<void> _startPush() async {
    final service = ref.read(pushServiceProvider);
    if (identical(service, _pushService) || identical(service, _pushStarting)) {
      return;
    }
    _pushStarting = service;
    await _foregroundSubscription?.cancel();
    await _openedSubscription?.cancel();
    await service.prepare();
    if (!mounted) return;
    if (identical(ref.read(pushServiceProvider), service)) {
      _pushService = service;
      _foregroundSubscription = service.foregroundMessages.listen(
        _onForegroundMessage,
      );
      _openedSubscription = service.openedMessages.listen(_onNotificationTap);
      final initial = await service.initialMessage();
      if (initial != null && mounted) _onNotificationTap(initial);
    }
    if (identical(_pushStarting, service)) _pushStarting = null;
    // The ledger address changed while Firebase was starting; wire up again.
    if (!identical(ref.read(pushServiceProvider), service)) {
      unawaited(_startPush());
    }
  }

  /// A new entry was announced while the app is open.
  void _onForegroundMessage(RemoteMessage message) {
    _refreshLedger();
  }

  /// The user tapped a notification: refresh the ledger and open the entry it
  /// names, or fall back to the ledger tab.
  void _onNotificationTap(RemoteMessage message) {
    _refreshLedger();
    final server = ref.read(serverControllerProvider);
    final uri = notificationEntryUri(message.data, server.serverUrl);
    if (uri == null) {
      if (server.status == ServerStatus.ready) {
        ref.read(homeTabProvider.notifier).select(0);
      }
      return;
    }
    _handleLink(uri);
  }

  void _refreshLedger() {
    if (ref.read(serverControllerProvider).status != ServerStatus.ready) return;
    unawaited(ref.read(ledgerProvider.notifier).refresh());
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
    ref.listen(pushServiceProvider, (previous, next) {
      if (!identical(previous, next)) unawaited(_startPush());
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
