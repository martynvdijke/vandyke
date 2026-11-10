import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:permission_handler/permission_handler.dart';

/// The client-side downscale applied before upload: at most 2400 px on the
/// longest side, encoded as JPEG at quality 85. This keeps a normal phone
/// photo inside the server's 10 MiB and 12,000 px limits.
const int photoMaxDimension = 2400;
const int photoJpegQuality = 85;

/// Picks one photo from the camera or the gallery with the downscale applied.
/// Injectable so widget tests never touch the platform picker.
typedef PhotoPicker = Future<XFile?> Function(ImageSource source);

Future<XFile?> defaultPhotoPicker(ImageSource source) {
  return ImagePicker().pickImage(
    source: source,
    maxWidth: photoMaxDimension.toDouble(),
    maxHeight: photoMaxDimension.toDouble(),
    imageQuality: photoJpegQuality,
  );
}

final photoPickerProvider = Provider<PhotoPicker>((ref) => defaultPhotoPicker);

/// Asks the OS for camera access before capture. Injectable so the denied
/// path can be exercised without a platform channel.
typedef CameraPermissionRequest = Future<bool> Function();

Future<bool> requestCameraPermission() async {
  final status = await Permission.camera.request();
  return status.isGranted || status.isLimited;
}

final cameraPermissionProvider = Provider<CameraPermissionRequest>(
  (ref) => requestCameraPermission,
);
