import 'package:intl/intl.dart';

/// Ledger dates are shown in the reader's local time, editorial style.
String formatLedgerDate(DateTime value) {
  final local = value.toLocal();
  final now = DateTime.now();
  final sameYear = local.year == now.year;
  return DateFormat(sameYear ? 'd MMM' : 'd MMM yyyy').format(local);
}

String pluralize(int count, String singular, [String? plural]) {
  if (count == 1) return '$count $singular';
  return '$count ${plural ?? '${singular}s'}';
}
