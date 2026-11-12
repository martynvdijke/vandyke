import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';
import '../server_controller.dart';
import 'push_service.dart';

final pushServiceProvider = Provider<PushService>((ref) {
  final service = PushService(ref.watch(apiClientProvider));
  ref.onDispose(service.dispose);
  return service;
});

/// What the settings switch may offer.
class NotificationState {
  const NotificationState({
    this.available = false,
    this.enabled = false,
    this.busy = false,
    this.error = '',
  });

  /// The server sent a complete Firebase configuration.
  final bool available;
  final bool enabled;

  /// A change is being applied.
  final bool busy;
  final String error;

  NotificationState copyWith({
    bool? available,
    bool? enabled,
    bool? busy,
    String? error,
  }) {
    return NotificationState(
      available: available ?? this.available,
      enabled: enabled ?? this.enabled,
      busy: busy ?? this.busy,
      error: error ?? this.error,
    );
  }
}

final notificationControllerProvider =
    NotifierProvider<NotificationController, NotificationState>(
      NotificationController.new,
    );

class NotificationController extends Notifier<NotificationState> {
  @override
  NotificationState build() {
    final server = ref.watch(serverControllerProvider);
    if (server.status != ServerStatus.ready) {
      return const NotificationState();
    }
    scheduleMicrotask(refresh);
    return const NotificationState();
  }

  PushService get _service => ref.read(pushServiceProvider);

  /// Asks the server whether push is configured and reads the saved choice.
  /// Any failure leaves the toggle hidden rather than raising an error.
  Future<void> refresh() async {
    await _service.prepare();
    final config = await _service.fetchConfig();
    final available = config?.usable ?? false;
    final enabled = available && await _service.isEnabled(config: config);
    state = NotificationState(available: available, enabled: enabled);
  }

  Future<void> setEnabled(bool value) async {
    if (state.busy || !state.available) return;
    state = state.copyWith(busy: true, error: '');
    if (value) {
      final started = await _service.enable();
      if (!started) {
        state = state.copyWith(
          busy: false,
          enabled: false,
          error: 'Notifications could not be turned on. Try again later.',
        );
        return;
      }
      state = state.copyWith(busy: false, enabled: true);
      return;
    }
    await _service.disable();
    state = state.copyWith(busy: false, enabled: false);
  }
}
