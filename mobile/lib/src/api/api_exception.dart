/// A typed failure returned by, or while reaching, the ledger API.
class ApiException implements Exception {
  const ApiException(
    this.message, {
    this.statusCode,
    this.code,
    this.retryAfterSeconds,
  });

  final String message;
  final int? statusCode;
  final String? code;

  /// Seconds the server asked the caller to wait, from `Retry-After`.
  final int? retryAfterSeconds;

  bool get isRateLimited => statusCode == 429;
  bool get isNotFound => statusCode == 404;
  bool get isValidation => statusCode == 422;
  bool get isDuplicate => statusCode == 409;

  /// The body or an attached image exceeded the server's limits.
  bool get isPayloadTooLarge => statusCode == 413;

  /// A message suitable for a full-width error panel.
  String get friendlyMessage {
    if (isPayloadTooLarge) {
      return 'That image is too large for the ledger. Try a smaller photo.';
    }
    if (isRateLimited) {
      final wait = retryAfterSeconds;
      final suffix = wait == null
          ? 'Give it a moment and try again.'
          : 'Try again in ${_duration(wait)}.';
      return 'The ledger is taking a breather. $suffix';
    }
    if (isDuplicate) {
      return 'That exact entry is already in the ledger.';
    }
    if (isNotFound) {
      return 'That ledger entry could not be found.';
    }
    if (isValidation) return message;
    if (statusCode != null && statusCode! >= 500) {
      return 'The ledger is having trouble right now. Try again shortly.';
    }
    return message;
  }

  static String _duration(int seconds) {
    if (seconds < 60) return '$seconds seconds';
    final minutes = (seconds / 60).ceil();
    return minutes == 1 ? 'a minute' : '$minutes minutes';
  }

  @override
  String toString() => message;
}
