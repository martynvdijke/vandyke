import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../config.dart';
import '../server_controller.dart';
import '../theme/tokens.dart';
import '../widgets/common.dart';

class SetupScreen extends ConsumerStatefulWidget {
  const SetupScreen({super.key, this.canCancel = false});

  /// When opened from settings the user can back out without changing servers.
  final bool canCancel;

  @override
  ConsumerState<SetupScreen> createState() => _SetupScreenState();
}

class _SetupScreenState extends ConsumerState<SetupScreen> {
  late final TextEditingController _server;

  @override
  void initState() {
    super.initState();
    final current = ref.read(serverControllerProvider).serverUrl;
    _server = TextEditingController(
      text: current.isEmpty ? defaultServerUrl : current,
    );
  }

  @override
  void dispose() {
    _server.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    FocusScope.of(context).unfocus();
    final saved = await ref
        .read(serverControllerProvider.notifier)
        .save(_server.text);
    if (!mounted) return;
    if (saved && widget.canCancel) Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final isDark = theme.brightness == Brightness.dark;
    final server = ref.watch(serverControllerProvider);
    return Scaffold(
      body: LayoutBuilder(
        builder: (context, constraints) {
          final wide = constraints.maxWidth > 700;
          return SingleChildScrollView(
            child: ConstrainedBox(
              constraints: BoxConstraints(minHeight: constraints.maxHeight),
              child: Column(
                children: [
                  Container(
                    width: double.infinity,
                    padding: EdgeInsets.fromLTRB(
                      wide ? 48 : 24,
                      MediaQuery.paddingOf(context).top + 28,
                      wide ? 48 : 24,
                      30,
                    ),
                    decoration: BoxDecoration(
                      gradient: LinearGradient(
                        begin: Alignment.topLeft,
                        end: Alignment.bottomRight,
                        colors: isDark
                            ? [const Color(0xFF1E3A2F), VandykeColors.night]
                            : [VandykeColors.greenDeep, VandykeColors.green],
                      ),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            const VandykeLogo(size: 42, onDark: true),
                            const SizedBox(width: 12),
                            Text(
                              'The vanDyke Ledger',
                              style: TextStyle(
                                fontFamily: 'Newsreader',
                                fontSize: 23,
                                fontWeight: FontWeight.w600,
                                color: VandykeColors.cream,
                              ),
                            ),
                            const Spacer(),
                            if (widget.canCancel)
                              IconButton(
                                tooltip: 'Cancel',
                                onPressed: () => Navigator.of(context).pop(),
                                icon: const Icon(
                                  Icons.close,
                                  color: VandykeColors.cream,
                                ),
                              ),
                          ],
                        ),
                        const SizedBox(height: 34),
                        Text(
                          'THE LANCO CORRECTIONS LEDGER',
                          style: theme.textTheme.labelSmall?.copyWith(
                            color: VandykeColors.cream.withValues(alpha: .7),
                            letterSpacing: 2,
                          ),
                        ),
                        const SizedBox(height: 10),
                        Text(
                          'Every misspelling,\ncarefully filed.',
                          style: TextStyle(
                            fontFamily: 'Newsreader',
                            fontSize: 38,
                            height: 1.02,
                            letterSpacing: -1.3,
                            fontWeight: FontWeight.w500,
                            color: VandykeColors.cream,
                          ),
                        ),
                        const SizedBox(height: 12),
                        Text(
                          'Point the pocket ledger at your vanDyke server to '
                          'search the record and log new sightings on the go.',
                          style: theme.textTheme.bodyMedium?.copyWith(
                            color: VandykeColors.cream.withValues(alpha: .8),
                            height: 1.5,
                          ),
                        ),
                      ],
                    ),
                  ),
                  Padding(
                    padding: EdgeInsets.symmetric(
                      horizontal: wide ? 48 : 22,
                      vertical: 26,
                    ),
                    child: Center(
                      child: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 520),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            const Eyebrow('Ledger address'),
                            const SizedBox(height: 10),
                            TextField(
                              controller: _server,
                              enabled: !server.busy,
                              keyboardType: TextInputType.url,
                              autocorrect: false,
                              textInputAction: TextInputAction.done,
                              onSubmitted: (_) => _connect(),
                              decoration: const InputDecoration(
                                hintText: 'https://vandyke.thor.edu',
                                prefixIcon: Icon(Icons.dns_outlined),
                              ),
                            ),
                            const SizedBox(height: 10),
                            Text(
                              'Use the address you open in a browser. HTTPS '
                              'is assumed when no scheme is given.',
                              style: theme.textTheme.bodyMedium?.copyWith(
                                color: scheme.onSurfaceVariant,
                              ),
                            ),
                            if (server.error.isNotEmpty) ...[
                              const SizedBox(height: 16),
                              ErrorPanel(message: server.error),
                            ],
                            const SizedBox(height: 22),
                            FilledButton(
                              onPressed: server.busy ? null : _connect,
                              child: server.busy
                                  ? const SizedBox(
                                      width: 18,
                                      height: 18,
                                      child: CircularProgressIndicator(
                                        strokeWidth: 2,
                                        color: VandykeColors.surface,
                                      ),
                                    )
                                  : const Text('Open the ledger'),
                            ),
                            const SizedBox(height: 16),
                            Text(
                              'The address is checked against the ledger API '
                              'before it is saved, and kept in this device’s '
                              'secure storage.',
                              textAlign: TextAlign.center,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: scheme.onSurfaceVariant,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          );
        },
      ),
    );
  }
}

/// Exposed so the settings screen can describe the active address consistently.
String normalizedServerLabel(String raw) => ApiClient.normalizeServerUrl(raw);
