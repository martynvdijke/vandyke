/// The public vanDyke ledger the app connects to on first run.
const String defaultServerUrl = 'https://vandyke.thor.edu';

/// Returns the server URL to prefill, preferring a previously saved address.
String resolveInitialServerUrl(String? saved) {
  final value = saved?.trim() ?? '';
  return value.isEmpty ? defaultServerUrl : value;
}
