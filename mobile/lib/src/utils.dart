import 'package:intl/intl.dart';

/// Ledger dates are shown in the reader's local time, editorial style.
String formatLedgerDate(DateTime value) {
  final local = value.toLocal();
  final now = DateTime.now();
  final sameYear = local.year == now.year;
  return DateFormat(sameYear ? 'd MMM' : 'd MMM yyyy').format(local);
}

/// A tighter stamp for the recent strip: relative for the last week, then a
/// short editorial date.
String formatShortDate(DateTime value) {
  final local = value.toLocal();
  final now = DateTime.now();
  final days = DateTime(
    now.year,
    now.month,
    now.day,
  ).difference(DateTime(local.year, local.month, local.day)).inDays;
  if (days == 0) return 'Today';
  if (days == 1) return 'Yesterday';
  if (days > 1 && days < 7) return '${days}d ago';
  return formatLedgerDate(local);
}

String pluralize(int count, String singular, [String? plural]) {
  if (count == 1) return '$count $singular';
  return '$count ${plural ?? '${singular}s'}';
}
