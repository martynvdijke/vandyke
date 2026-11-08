import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../server_controller.dart';
import '../theme/theme_controller.dart';
import '../widgets/common.dart';
import 'setup_screen.dart';

class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  Future<void> _openWebLedger(BuildContext context, String serverUrl) async {
    final uri = Uri.tryParse(serverUrl);
    if (uri == null) return;
    final opened = await launchUrl(uri, mode: LaunchMode.externalApplication);
    if (!opened && context.mounted) {
      showVandykeSnack(context, 'Could not open the web ledger.', error: true);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final server = ref.watch(serverControllerProvider);
    final mode = ref.watch(themeControllerProvider);
    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(22, 18, 22, 32),
          children: [
            const Eyebrow('Pocket ledger'),
            const SizedBox(height: 4),
            Text('Settings', style: theme.textTheme.displaySmall),
            const SizedBox(height: 4),
            Text(
              'Where the ledger lives and how it looks.',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 22),
            Container(
              padding: const EdgeInsets.all(18),
              decoration: BoxDecoration(
                color: scheme.surface,
                borderRadius: BorderRadius.circular(18),
                border: Border.all(color: scheme.outlineVariant),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Eyebrow('Ledger address'),
                  const SizedBox(height: 8),
                  Text(
                    normalizedServerLabel(server.serverUrl),
                    style: theme.textTheme.titleMedium,
                  ),
                  const SizedBox(height: 6),
                  Text(
                    'The address is held in this device’s secure storage.',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 16),
                  Row(
                    children: [
                      OutlinedButton(
                        onPressed: () => Navigator.of(context).push(
                          MaterialPageRoute<void>(
                            builder: (_) => const SetupScreen(canCancel: true),
                          ),
                        ),
                        child: const Text('Change address'),
                      ),
                      const SizedBox(width: 10),
                      TextButton(
                        onPressed: () => _openWebLedger(
                          context,
                          normalizedServerLabel(server.serverUrl),
                        ),
                        child: const Text('Open web ledger'),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(height: 14),
            Container(
              padding: const EdgeInsets.all(18),
              decoration: BoxDecoration(
                color: scheme.surface,
                borderRadius: BorderRadius.circular(18),
                border: Border.all(color: scheme.outlineVariant),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Eyebrow('Appearance'),
                  const SizedBox(height: 10),
                  SegmentedButton<ThemeMode>(
                    segments: const [
                      ButtonSegment(
                        value: ThemeMode.system,
                        label: Text('System'),
                        icon: Icon(Icons.brightness_auto_outlined, size: 18),
                      ),
                      ButtonSegment(
                        value: ThemeMode.light,
                        label: Text('Light'),
                        icon: Icon(Icons.light_mode_outlined, size: 18),
                      ),
                      ButtonSegment(
                        value: ThemeMode.dark,
                        label: Text('Dark'),
                        icon: Icon(Icons.dark_mode_outlined, size: 18),
                      ),
                    ],
                    selected: {mode},
                    showSelectedIcon: false,
                    onSelectionChanged: (selection) => ref
                        .read(themeControllerProvider.notifier)
                        .set(selection.first),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 14),
            Container(
              padding: const EdgeInsets.all(18),
              decoration: BoxDecoration(
                color: scheme.surface,
                borderRadius: BorderRadius.circular(18),
                border: Border.all(color: scheme.outlineVariant),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Eyebrow('About'),
                  const SizedBox(height: 8),
                  Text(
                    'The vanDyke Corrections Ledger',
                    style: theme.textTheme.titleMedium,
                  ),
                  const SizedBox(height: 4),
                  Text(
                    'A pocket field guide to the many ways the name gets '
                    'misspelled. Version 0.1.0.',
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 14),
                  TextButton(
                    onPressed: () async {
                      final confirmed = await showDialog<bool>(
                        context: context,
                        builder: (context) => AlertDialog(
                          title: const Text('Forget this server?'),
                          content: const Text(
                            'The saved address will be removed from this '
                            'device. You can reconnect at any time.',
                          ),
                          actions: [
                            TextButton(
                              onPressed: () => Navigator.of(context).pop(false),
                              child: const Text('Cancel'),
                            ),
                            FilledButton(
                              onPressed: () => Navigator.of(context).pop(true),
                              child: const Text('Forget'),
                            ),
                          ],
                        ),
                      );
                      if (confirmed == true) {
                        await ref
                            .read(serverControllerProvider.notifier)
                            .forgetServer();
                      }
                    },
                    child: const Text('Disconnect'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
