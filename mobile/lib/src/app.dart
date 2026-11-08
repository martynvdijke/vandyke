import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'screens/home_shell.dart';
import 'screens/setup_screen.dart';
import 'server_controller.dart';
import 'theme/theme_controller.dart';
import 'theme/vandyke_theme.dart';
import 'widgets/common.dart';

class VandykeApp extends ConsumerWidget {
  const VandykeApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final server = ref.watch(serverControllerProvider);
    final mode = ref.watch(themeControllerProvider);
    return MaterialApp(
      title: 'The vanDyke Ledger',
      debugShowCheckedModeBanner: false,
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
