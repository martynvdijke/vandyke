import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'src/app.dart';
import 'src/notifications/push_service.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  // Registered before the app starts so a data message that wakes the
  // background isolate finds a handler. The handler only logs; the tray draws
  // notification messages by itself.
  FirebaseMessaging.onBackgroundMessage(firebaseMessagingBackgroundHandler);
  runApp(const ProviderScope(child: VandykeApp()));
}
