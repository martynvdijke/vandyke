import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;

import 'api/api_client.dart';
import 'api/api_exception.dart';
import 'config.dart';
import 'storage.dart';

enum ServerStatus { loading, unconfigured, ready }

class ServerState {
  const ServerState({
    required this.status,
    this.serverUrl = defaultServerUrl,
    this.error = '',
    this.busy = false,
  });

  final ServerStatus status;
  final String serverUrl;
  final String error;

  /// True while an address is being validated and saved.
  final bool busy;

  ServerState copyWith({
    ServerStatus? status,
    String? serverUrl,
    String? error,
    bool? busy,
  }) {
    return ServerState(
      status: status ?? this.status,
      serverUrl: serverUrl ?? this.serverUrl,
      error: error ?? this.error,
      busy: busy ?? this.busy,
    );
  }
}

final serverControllerProvider =
    NotifierProvider<ServerController, ServerState>(ServerController.new);

class ServerController extends Notifier<ServerState> {
  @override
  ServerState build() {
    scheduleMicrotask(_restore);
    return const ServerState(status: ServerStatus.loading);
  }

  AppStorage get _storage => ref.read(appStorageProvider);

  Future<void> _restore() async {
    String? saved;
    try {
      saved = await _storage.readServer();
    } catch (_) {
      saved = null;
    }
    state = ServerState(
      status: (saved == null || saved.trim().isEmpty)
          ? ServerStatus.unconfigured
          : ServerStatus.ready,
      serverUrl: resolveInitialServerUrl(saved),
    );
  }

  /// Validates the address against `/api/v1/stats` before saving it, so a typo
  /// never replaces a working configuration.
  Future<bool> save(String raw, {http.Client? client}) async {
    final server = ApiClient.normalizeServerUrl(raw);
    if (server.isEmpty) {
      state = state.copyWith(error: 'Enter the ledger address.', busy: false);
      return false;
    }

    state = state.copyWith(busy: true, error: '');
    final probe = ApiClient(serverUrl: server, client: client);
    try {
      await probe.checkServer();
    } on ApiException catch (error) {
      state = state.copyWith(
        busy: false,
        error: error.friendlyMessage,
        serverUrl: server,
      );
      return false;
    } catch (_) {
      state = state.copyWith(
        busy: false,
        error: 'Could not reach that ledger. Check the address and try again.',
        serverUrl: server,
      );
      return false;
    } finally {
      probe.close();
    }

    try {
      await _storage.saveServer(server);
    } catch (_) {
      state = state.copyWith(
        busy: false,
        error: 'Could not save the address on this device.',
      );
      return false;
    }
    state = ServerState(status: ServerStatus.ready, serverUrl: server);
    return true;
  }

  Future<void> forgetServer() async {
    try {
      await _storage.clearServer();
    } catch (_) {
      // Clearing is best effort; the UI still returns to setup.
    }
    state = const ServerState(status: ServerStatus.unconfigured);
  }

  void clearError() {
    if (state.error.isNotEmpty) state = state.copyWith(error: '');
  }
}
