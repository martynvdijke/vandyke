import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/models.dart';
import '../providers.dart';
import '../utils.dart';
import '../widgets/common.dart';
import '../widgets/log_misspelling_sheet.dart';

/// "The ledger in numbers": headline counts, the run of the ledger, a compact
/// year chart and the usual suspects. Optional breakdowns disappear when an
/// older server doesn't report them.
class StatsScreen extends ConsumerWidget {
  const StatsScreen({super.key});

  Future<void> _refresh(WidgetRef ref) async {
    ref.invalidate(statsProvider);
    try {
      await ref.read(statsProvider.future);
    } catch (_) {
      // The error panel reports the failure; this only ends the spinner.
    }
  }

  /// A fresh ledger can start counting without leaving this screen.
  Future<void> _logMisspelling(BuildContext context, WidgetRef ref) async {
    final entry = await showLogMisspellingSheet(context);
    if (entry == null) return;
    await ref.read(ledgerProvider.notifier).acceptCreated(entry);
    if (!context.mounted) return;
    showVandykeSnack(context, '“${entry.word}” added to the ledger.');
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final stats = ref.watch(statsProvider);
    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: RefreshIndicator(
          onRefresh: () => _refresh(ref),
          child: ListView(
            physics: const AlwaysScrollableScrollPhysics(),
            padding: const EdgeInsets.fromLTRB(22, 18, 22, 40),
            children: [
              const Eyebrow('The record so far'),
              const SizedBox(height: 4),
              Text('Statistics', style: theme.textTheme.displaySmall),
              const SizedBox(height: 4),
              Text(
                'The ledger in numbers — every misspelling, counted.',
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 20),
              stats.when(
                loading: () => const _StatsSkeleton(),
                error: (error, _) => ErrorPanel(
                  message: error is Exception
                      ? error.toString()
                      : 'Could not load the ledger statistics.',
                  onRetry: () => ref.invalidate(statsProvider),
                ),
                data: (data) => data.total == 0
                    ? _StatsEmpty(onLog: () => _logMisspelling(context, ref))
                    : _StatsBody(stats: data),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _StatsBody extends StatelessWidget {
  const _StatsBody({required this.stats});

  final LedgerStats stats;

  @override
  Widget build(BuildContext context) {
    final photo = stats.withPhoto;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _HeroTotal(stats: stats),
        const SizedBox(height: 12),
        IntrinsicHeight(
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Expanded(
                child: _Panel(
                  eyebrow: 'Last 30 days',
                  value: formatCount(stats.entriesLast30Days),
                  note: 'New sightings logged',
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: photo == null
                    ? _Panel(
                        eyebrow: 'Unique spellings',
                        value: formatCount(stats.uniqueWords),
                        note: 'Distinct variants on file',
                      )
                    : _Panel(
                        eyebrow: 'With a photo',
                        value: stats.photoShareLabel,
                        note:
                            '${formatCount(photo)} of '
                            '${formatCount(stats.total)} slips',
                        meter: stats.hasPhotoShare ? stats.photoShare : null,
                      ),
              ),
            ],
          ),
        ),
        const SizedBox(height: 12),
        _SpanCard(stats: stats),
        if (stats.byYear.isNotEmpty) ...[
          const SizedBox(height: 12),
          _YearChart(years: stats.byYear),
        ],
        if (stats.topPersons.isNotEmpty) ...[
          const SizedBox(height: 12),
          _AuthorRanks(persons: stats.topPersons),
        ],
      ],
    );
  }
}

class _HeroTotal extends StatelessWidget {
  const _HeroTotal({required this.stats});

  final LedgerStats stats;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final unique = stats.uniqueWords;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(22, 20, 22, 20),
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
          Container(
            width: 26,
            height: 2.5,
            decoration: BoxDecoration(
              color: scheme.onPrimary.withValues(alpha: .5),
              borderRadius: BorderRadius.circular(999),
            ),
          ),
          const SizedBox(height: 10),
          Text(
            'MISSPELLINGS FILED',
            style: theme.textTheme.labelSmall?.copyWith(
              color: scheme.onPrimary.withValues(alpha: .78),
              letterSpacing: 1.8,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            formatCount(stats.total),
            style: TextStyle(
              fontFamily: 'Newsreader',
              fontSize: 62,
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
                : '${formatCount(unique)} different ways to get it wrong.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: scheme.onPrimary.withValues(alpha: .85),
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
    this.meter,
  });

  final String eyebrow;
  final String value;
  final String note;

  /// Optional 0…1 progress reading, drawn as a hairline meter.
  final double? meter;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final reading = meter;
    return _Card(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Eyebrow(eyebrow),
          const SizedBox(height: 8),
          FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Text(
              value,
              maxLines: 1,
              style: theme.textTheme.headlineLarge?.copyWith(
                fontFamily: 'Newsreader',
                fontSize: 32,
              ),
            ),
          ),
          const SizedBox(height: 4),
          Text(
            note,
            style: theme.textTheme.bodySmall?.copyWith(
              color: scheme.onSurfaceVariant,
            ),
          ),
          if (reading != null) ...[
            const SizedBox(height: 10),
            ClipRRect(
              borderRadius: BorderRadius.circular(999),
              child: LinearProgressIndicator(
                value: reading,
                minHeight: 3,
                backgroundColor: scheme.outlineVariant,
                color: scheme.primary,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _SpanCard extends StatelessWidget {
  const _SpanCard({required this.stats});

  final LedgerStats stats;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final hasDates = stats.firstEntryAt != null && stats.lastEntryAt != null;
    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Eyebrow('The span'),
          const SizedBox(height: 14),
          Row(
            children: [
              _SpanEnd(
                label: 'First entry',
                value: stats.firstEntryLabel,
                alignment: CrossAxisAlignment.start,
              ),
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 10),
                  child: Row(
                    children: [
                      Expanded(
                        child: Container(
                          height: 1,
                          color: scheme.outlineVariant,
                        ),
                      ),
                      Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 4),
                        child: Icon(
                          Icons.arrow_right_alt,
                          size: 16,
                          color: scheme.secondary,
                        ),
                      ),
                      Expanded(
                        child: Container(
                          height: 1,
                          color: scheme.outlineVariant,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              _SpanEnd(
                label: 'Most recent',
                value: stats.lastEntryLabel,
                alignment: CrossAxisAlignment.end,
              ),
            ],
          ),
          const SizedBox(height: 14),
          Text(
            hasDates
                ? 'Running for ${stats.runningLabel}.'
                : 'No dates recorded yet.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: scheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

class _SpanEnd extends StatelessWidget {
  const _SpanEnd({
    required this.label,
    required this.value,
    required this.alignment,
  });

  final String label;
  final String value;
  final CrossAxisAlignment alignment;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Column(
      crossAxisAlignment: alignment,
      children: [
        Text(
          value,
          maxLines: 1,
          style: theme.textTheme.titleLarge?.copyWith(
            fontFamily: 'Newsreader',
            fontSize: 17,
            fontWeight: FontWeight.w500,
          ),
        ),
        const SizedBox(height: 3),
        Text(
          label.toUpperCase(),
          style: theme.textTheme.labelSmall?.copyWith(
            fontSize: 9,
            letterSpacing: 1.1,
            color: scheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }
}

class _YearChart extends StatelessWidget {
  const _YearChart({required this.years});

  final List<YearCount> years;

  String get _rangeLabel {
    final first = years.first.year;
    final last = years.last.year;
    return first == last ? '$first' : '$first–$last';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final maxCount = years.fold<int>(1, (max, item) {
      return item.count > max ? item.count : max;
    });
    final currentYear = DateTime.now().year;
    final scale = MediaQuery.textScalerOf(context)
        .clamp(maxScaleFactor: 1.5)
        .scale(1);
    final chartHeight = 128 * scale;

    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(child: Eyebrow('Slips per year')),
              Text(
                _rangeLabel,
                style: theme.textTheme.labelSmall?.copyWith(
                  fontSize: 9.5,
                  letterSpacing: 0.8,
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          SizedBox(
            height: chartHeight,
            child: Padding(
              padding: EdgeInsets.only(top: 16 * scale),
              child: LayoutBuilder(
                builder: (context, constraints) {
                  final count = years.length;
                  const gap = 6.0;
                  final slot =
                      ((constraints.maxWidth - gap * (count - 1)) / count)
                          .clamp(26.0, 54.0);
                  final totalWidth = slot * count + gap * (count - 1);
                  final bars = <Widget>[
                    for (var i = 0; i < count; i++) ...[
                      if (i > 0) const SizedBox(width: gap),
                      SizedBox(
                        width: slot,
                        height: constraints.maxHeight,
                        child: _YearBar(
                          year: years[i],
                          maxCount: maxCount,
                          highlighted: years[i].year == currentYear,
                          scale: scale,
                        ),
                      ),
                    ],
                  ];
                  if (totalWidth > constraints.maxWidth) {
                    return SingleChildScrollView(
                      scrollDirection: Axis.horizontal,
                      child: Row(children: bars),
                    );
                  }
                  return Center(
                    child: Row(mainAxisSize: MainAxisSize.min, children: bars),
                  );
                },
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _YearBar extends StatelessWidget {
  const _YearBar({
    required this.year,
    required this.maxCount,
    required this.highlighted,
    required this.scale,
  });

  final YearCount year;
  final int maxCount;
  final bool highlighted;
  final double scale;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final dark = theme.brightness == Brightness.dark;
    final ratio = year.count <= 0
        ? 0.0
        : (year.count / maxCount).clamp(0.05, 1.0);
    final track = scheme.primary.withValues(alpha: dark ? .12 : .06);
    final fill = highlighted
        ? scheme.primary
        : scheme.primary.withValues(alpha: dark ? .45 : .30);
    return Semantics(
      label: '${year.year}: ${pluralize(year.count, 'slip', 'slips')}',
      child: ExcludeSemantics(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Expanded(
              child: LayoutBuilder(
                builder: (context, constraints) {
                  final height = constraints.maxHeight;
                  final fillHeight = ratio <= 0
                      ? 0.0
                      : (height * ratio).clamp(2.0, height);
                  return Stack(
                    clipBehavior: Clip.none,
                    children: [
                      Positioned.fill(
                        child: DecoratedBox(
                          decoration: BoxDecoration(
                            color: track,
                            borderRadius: BorderRadius.circular(5),
                          ),
                        ),
                      ),
                      if (fillHeight > 0)
                        Positioned(
                          left: 0,
                          right: 0,
                          bottom: 0,
                          height: fillHeight,
                          child: DecoratedBox(
                            decoration: BoxDecoration(
                              color: fill,
                              borderRadius: BorderRadius.circular(5),
                            ),
                          ),
                        ),
                      Positioned(
                        left: 0,
                        right: 0,
                        bottom: fillHeight + 4 * scale,
                        child: Text(
                          formatCount(year.count),
                          maxLines: 1,
                          textAlign: TextAlign.center,
                          style: theme.textTheme.labelSmall?.copyWith(
                            fontSize: 10,
                            letterSpacing: 0.2,
                            fontWeight: FontWeight.w700,
                            color: highlighted
                                ? scheme.onSurface
                                : scheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                    ],
                  );
                },
              ),
            ),
            SizedBox(height: 6 * scale),
            Text(
              '${year.year}',
              maxLines: 1,
              style: theme.textTheme.labelSmall?.copyWith(
                fontSize: 9.5,
                letterSpacing: 0.4,
                fontWeight: highlighted ? FontWeight.w700 : FontWeight.w600,
                color: highlighted ? scheme.primary : scheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _AuthorRanks extends StatelessWidget {
  const _AuthorRanks({required this.persons});

  final List<PersonCount> persons;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final maxCount = persons.fold<int>(1, (max, person) {
      return person.count > max ? person.count : max;
    });
    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(child: Eyebrow('Most frequent authors')),
              Text(
                'by slips filed',
                style: theme.textTheme.labelSmall?.copyWith(
                  fontSize: 9.5,
                  letterSpacing: 0.8,
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          for (var i = 0; i < persons.length; i++) ...[
            if (i > 0) const SizedBox(height: 4),
            _AuthorRow(rank: i + 1, person: persons[i], maxCount: maxCount),
          ],
        ],
      ),
    );
  }
}

class _AuthorRow extends StatelessWidget {
  const _AuthorRow({
    required this.rank,
    required this.person,
    required this.maxCount,
  });

  final int rank;
  final PersonCount person;
  final int maxCount;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final count = person.count.clamp(0, maxCount);
    final fillFlex = count < 1 ? 1 : count;
    final restFlex = maxCount - count < 1 ? 1 : maxCount - count;
    return Semantics(
      label: '${person.person}, ${pluralize(person.count, 'slip', 'slips')}',
      child: ExcludeSemantics(
        child: ClipRRect(
          borderRadius: BorderRadius.circular(7),
          child: Stack(
            children: [
              Positioned.fill(
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Expanded(
                      flex: fillFlex,
                      child: ColoredBox(color: scheme.primaryContainer),
                    ),
                    Expanded(flex: restFlex, child: const SizedBox.shrink()),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 9,
                ),
                child: Row(
                  children: [
                    SizedBox(
                      width: 20,
                      child: Text(
                        '$rank',
                        style: theme.textTheme.titleMedium?.copyWith(
                          fontFamily: 'Newsreader',
                          fontSize: 14,
                          fontWeight: FontWeight.w500,
                          color: scheme.secondary,
                        ),
                      ),
                    ),
                    Expanded(
                      child: Text(
                        person.person,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodyMedium?.copyWith(
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Text(
                      formatCount(person.count),
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontSize: 14,
                        fontWeight: FontWeight.w600,
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

class _StatsSkeleton extends StatelessWidget {
  const _StatsSkeleton();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          'Gathering the numbers…',
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 12),
        const _SkeletonBlock(height: 178, radius: 20, emphasis: true),
        const SizedBox(height: 12),
        const Row(
          children: [
            Expanded(child: _SkeletonBlock(height: 118)),
            SizedBox(width: 12),
            Expanded(child: _SkeletonBlock(height: 118)),
          ],
        ),
        const SizedBox(height: 12),
        const _SkeletonBlock(height: 138),
        const SizedBox(height: 12),
        const _SkeletonBlock(height: 196),
      ],
    );
  }
}

class _SkeletonBlock extends StatelessWidget {
  const _SkeletonBlock({
    required this.height,
    this.radius = 18,
    this.emphasis = false,
  });

  final double height;
  final double radius;

  /// The hero block keeps a tint of the green it will become.
  final bool emphasis;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final dark = theme.brightness == Brightness.dark;
    return Container(
      height: height,
      decoration: BoxDecoration(
        color: emphasis
            ? scheme.primary.withValues(alpha: dark ? .2 : .12)
            : scheme.surface,
        borderRadius: BorderRadius.circular(radius),
        border: Border.all(
          color: emphasis
              ? scheme.primary.withValues(alpha: dark ? .28 : .18)
              : scheme.outlineVariant.withValues(alpha: .7),
        ),
      ),
    );
  }
}

class _StatsEmpty extends StatelessWidget {
  const _StatsEmpty({required this.onLog});

  final VoidCallback onLog;

  @override
  Widget build(BuildContext context) {
    return EmptyState(
      title: 'Nothing counted yet',
      message:
          'The ledger is blank. Log the first misspelling and the numbers '
          'will start to take shape.',
      icon: Icons.insights_outlined,
      actionLabel: 'Log a misspelling',
      onAction: onLog,
    );
  }
}

/// One shared card shell so every panel keeps the same paper edge.
class _Card extends StatelessWidget {
  const _Card({required this.child, this.padding = const EdgeInsets.all(20)});

  final Widget child;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: padding,
      decoration: BoxDecoration(
        color: scheme.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: child,
    );
  }
}
