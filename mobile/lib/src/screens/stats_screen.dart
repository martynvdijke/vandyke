import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';
import '../widgets/common.dart';

class StatsScreen extends ConsumerWidget {
  const StatsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final stats = ref.watch(statsProvider);
    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: RefreshIndicator(
          onRefresh: () async => ref.invalidate(statsProvider),
          child: ListView(
            physics: const AlwaysScrollableScrollPhysics(),
            padding: const EdgeInsets.fromLTRB(22, 18, 22, 32),
            children: [
              const Eyebrow('The record so far'),
              const SizedBox(height: 4),
              Text('Statistics', style: theme.textTheme.displaySmall),
              const SizedBox(height: 4),
              Text(
                'A quiet measure of how often the name escapes intact.',
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 20),
              stats.when(
                loading: () => const Padding(
                  padding: EdgeInsets.symmetric(vertical: 48),
                  child: Center(
                    child: SizedBox(
                      width: 22,
                      height: 22,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  ),
                ),
                error: (error, _) => ErrorPanel(
                  message: error is Exception
                      ? error.toString()
                      : 'Could not load the ledger statistics.',
                  onRetry: () => ref.invalidate(statsProvider),
                ),
                data: (data) => Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    _HeroTotal(total: data.total, unique: data.uniqueWords),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: _Panel(
                            eyebrow: 'Last 30 days',
                            value: '${data.entriesLast30Days}',
                            note: 'New sightings logged',
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: _Panel(
                            eyebrow: 'Unique spellings',
                            value: '${data.uniqueWords}',
                            note: 'Distinct variants on file',
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    Container(
                      padding: const EdgeInsets.all(20),
                      decoration: BoxDecoration(
                        color: scheme.surface,
                        borderRadius: BorderRadius.circular(18),
                        border: Border.all(color: scheme.outlineVariant),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Eyebrow('The archive'),
                          const SizedBox(height: 14),
                          _Row(
                            label: 'First entry',
                            value: data.firstEntryLabel,
                          ),
                          const SizedBox(height: 10),
                          _Row(
                            label: 'Most recent',
                            value: data.lastEntryLabel,
                          ),
                          const SizedBox(height: 10),
                          _Row(label: 'Total entries', value: '${data.total}'),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HeroTotal extends StatelessWidget {
  const _HeroTotal({required this.total, required this.unique});

  final int total;
  final int unique;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(22, 22, 22, 20),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [scheme.primary, scheme.primary.withValues(alpha: .78)],
        ),
        borderRadius: BorderRadius.circular(20),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'MISSPELLINGS FILED',
            style: theme.textTheme.labelSmall?.copyWith(
              color: scheme.onPrimary.withValues(alpha: .75),
              letterSpacing: 1.8,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            '$total',
            style: TextStyle(
              fontFamily: 'Newsreader',
              fontSize: 64,
              height: 0.95,
              letterSpacing: -2,
              fontWeight: FontWeight.w500,
              color: scheme.onPrimary,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            unique == 1
                ? 'One singular way to get it wrong.'
                : '$unique different ways to get it wrong.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: scheme.onPrimary.withValues(alpha: .82),
            ),
          ),
        ],
      ),
    );
  }
}

class _Panel extends StatelessWidget {
  const _Panel({
    required this.eyebrow,
    required this.value,
    required this.note,
  });

  final String eyebrow;
  final String value;
  final String note;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: scheme.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Eyebrow(eyebrow),
          const SizedBox(height: 8),
          Text(
            value,
            style: theme.textTheme.headlineLarge?.copyWith(
              fontFamily: 'Newsreader',
              fontSize: 32,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            note,
            style: theme.textTheme.bodySmall?.copyWith(
              color: scheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

class _Row extends StatelessWidget {
  const _Row({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: [
        Text(
          label,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const Spacer(),
        Text(value, style: theme.textTheme.titleMedium),
      ],
    );
  }
}
