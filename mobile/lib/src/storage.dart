import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Persists the ledger address in secure storage and the small UI preference
/// in shared preferences.
class AppStorage {
  AppStorage({FlutterSecureStorage? secure})
    : _secure = secure ?? const FlutterSecureStorage();

  static const _serverKey = 'vandyke.server';
  static const _themeKey = 'vandyke.theme';

  final FlutterSecureStorage _secure;

  Future<String?> readServer() => _secure.read(key: _serverKey);

  Future<void> saveServer(String serverUrl) async {
    await _secure.write(key: _serverKey, value: serverUrl);
  }

  Future<void> clearServer() async {
    await _secure.delete(key: _serverKey);
  }

  Future<String> readTheme() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_themeKey) ?? 'system';
  }

  Future<void> writeTheme(String value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_themeKey, value);
  }
}

final appStorageProvider = Provider<AppStorage>((ref) => AppStorage());
