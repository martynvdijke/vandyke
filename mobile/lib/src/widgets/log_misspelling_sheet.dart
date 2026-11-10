import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../api/api_exception.dart';
import '../api/models.dart';
import '../photo.dart';
import '../providers.dart';
import '../theme/tokens.dart';
import 'common.dart';

/// Opens the quick-add sheet and returns the entry that was created.
Future<Entry?> showLogMisspellingSheet(BuildContext context) {
  return showModalBottomSheet<Entry>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => const LogMisspellingSheet(),
  );
}

class LogMisspellingSheet extends ConsumerStatefulWidget {
  const LogMisspellingSheet({super.key});

  @override
  ConsumerState<LogMisspellingSheet> createState() =>
      _LogMisspellingSheetState();
}

class _LogMisspellingSheetState extends ConsumerState<LogMisspellingSheet> {
  final _formKey = GlobalKey<FormState>();
  final _word = TextEditingController();
  final _meaning = TextEditingController();
  final _person = TextEditingController();
  bool _busy = false;
  String _error = '';
  XFile? _photo;
  double? _progress;

  @override
  void dispose() {
    _word.dispose();
    _meaning.dispose();
    _person.dispose();
    super.dispose();
  }

  /// Captures or selects one photo. A cancelled picker or a denied camera
  /// permission leaves the current attachment and the text fields untouched.
  Future<void> _pickPhoto(ImageSource source) async {
    if (_busy) return;
    if (source == ImageSource.camera) {
      var granted = false;
      try {
        granted = await ref.read(cameraPermissionProvider)();
      } catch (_) {
        granted = false;
      }
      if (!granted) {
        if (mounted) {
          showVandykeSnack(
            context,
            'Camera access is off. Allow it in Android settings, or choose a '
            'photo from the gallery.',
            error: true,
          );
        }
        return;
      }
    }

    XFile? picked;
    try {
      picked = await ref.read(photoPickerProvider)(source);
    } catch (_) {
      if (mounted) {
        showVandykeSnack(
          context,
          'Could not open the ${source == ImageSource.camera ? 'camera' : 'gallery'}.',
          error: true,
        );
      }
      return;
    }
    if (picked == null) return;
    if (!mounted) return;
    setState(() => _photo = picked);
  }

  void _removePhoto() {
    if (_busy) return;
    setState(() => _photo = null);
  }

  Future<void> _submit() async {
    FocusScope.of(context).unfocus();
    if (!(_formKey.currentState?.validate() ?? false)) return;
    setState(() {
      _busy = true;
      _error = '';
      _progress = null;
    });
    final photo = _photo;
    try {
      final entry = await ref
          .read(apiClientProvider)
          .createEntry(
            word: _word.text,
            meaning: _meaning.text,
            person: _person.text,
            photoPath: photo?.path,
            onProgress: photo == null
                ? null
                : (sent, total) {
                    if (!mounted) return;
                    setState(() {
                      _progress = total > 0
                          ? (sent / total).clamp(0.0, 1.0)
                          : null;
                    });
                  },
          );
      if (mounted) Navigator.of(context).pop(entry);
    } on ApiException catch (error) {
      if (mounted) {
        setState(() {
          _busy = false;
          _progress = null;
          _error = error.friendlyMessage;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _busy = false;
          _progress = null;
          _error = 'Could not reach the ledger. Check your connection.';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final photo = _photo;
    return Padding(
      padding: EdgeInsets.only(
        left: 22,
        right: 22,
        top: 10,
        bottom: MediaQuery.viewInsetsOf(context).bottom + 24,
      ),
      child: SingleChildScrollView(
        child: Form(
          key: _formKey,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Center(
                child: Container(
                  width: 40,
                  height: 4,
                  decoration: BoxDecoration(
                    color: scheme.outlineVariant,
                    borderRadius: BorderRadius.circular(999),
                  ),
                ),
              ),
              const SizedBox(height: 22),
              Text(
                'FIELD NOTE',
                style: theme.textTheme.labelSmall?.copyWith(
                  letterSpacing: 1.8,
                  color: scheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'Log a new misspelling',
                style: theme.textTheme.headlineSmall?.copyWith(
                  fontSize: 24,
                  fontFamily: 'Newsreader',
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'Spotted the name butchered in the wild? Add it to the ledger.',
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 22),
              TextFormField(
                controller: _word,
                enabled: !_busy,
                autofocus: true,
                maxLength: 200,
                textCapitalization: TextCapitalization.words,
                textInputAction: TextInputAction.next,
                decoration: const InputDecoration(
                  labelText: 'The misspelling',
                  hintText: 'e.g. Van Dyke, Vandyck, vanDike',
                  counterText: '',
                ),
                validator: (value) => (value ?? '').trim().isEmpty
                    ? 'Add the misspelling you spotted.'
                    : null,
              ),
              const SizedBox(height: 14),
              TextFormField(
                controller: _meaning,
                enabled: !_busy,
                maxLength: 500,
                maxLines: 2,
                minLines: 2,
                textInputAction: TextInputAction.next,
                decoration: const InputDecoration(
                  labelText: 'What it should have been',
                  hintText: 'A short note for the record',
                  counterText: '',
                ),
                validator: (value) => (value ?? '').trim().isEmpty
                    ? 'Add a short correction or context note.'
                    : null,
              ),
              const SizedBox(height: 14),
              TextFormField(
                controller: _person,
                enabled: !_busy,
                maxLength: 100,
                textCapitalization: TextCapitalization.words,
                textInputAction: TextInputAction.done,
                onFieldSubmitted: (_) => _submit(),
                decoration: const InputDecoration(
                  labelText: 'Person (optional)',
                  hintText: 'Who miswrote it?',
                  counterText: '',
                ),
              ),
              const SizedBox(height: 16),
              _PhotoField(
                photo: photo,
                busy: _busy,
                onCamera: () => _pickPhoto(ImageSource.camera),
                onGallery: () => _pickPhoto(ImageSource.gallery),
                onRemove: _removePhoto,
              ),
              if (_busy && photo != null) ...[
                const SizedBox(height: 14),
                LinearProgressIndicator(
                  key: const ValueKey('upload-progress'),
                  value: _progress,
                  minHeight: 4,
                  backgroundColor: scheme.surfaceContainerHigh,
                ),
                const SizedBox(height: 6),
                Text(
                  _progress == null
                      ? 'Uploading photo…'
                      : 'Uploading photo… ${(_progress! * 100).round()}%',
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                    letterSpacing: 0.6,
                  ),
                ),
              ],
              if (_error.isNotEmpty) ...[
                const SizedBox(height: 14),
                Container(
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: scheme.errorContainer.withValues(alpha: .35),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(
                      color: scheme.error.withValues(alpha: .3),
                    ),
                  ),
                  child: Text(
                    _error,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: scheme.onSurface,
                    ),
                  ),
                ),
              ],
              const SizedBox(height: 20),
              FilledButton(
                onPressed: _busy ? null : _submit,
                child: _busy
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: VandykeColors.surface,
                        ),
                      )
                    : const Text('Add to the ledger'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The attachment strip: sources while empty, a preview plus replace and
/// remove actions once a photo is chosen.
class _PhotoField extends StatelessWidget {
  const _PhotoField({
    required this.photo,
    required this.busy,
    required this.onCamera,
    required this.onGallery,
    required this.onRemove,
  });

  final XFile? photo;
  final bool busy;
  final VoidCallback onCamera;
  final VoidCallback onGallery;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final attachment = photo;
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 10, 8, 12),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  attachment == null
                      ? 'PHOTO EVIDENCE (OPTIONAL)'
                      : 'PHOTO ATTACHED',
                  style: theme.textTheme.labelSmall?.copyWith(
                    letterSpacing: 1.4,
                    fontWeight: FontWeight.w700,
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ),
              IconButton(
                key: const ValueKey('photo-camera'),
                tooltip: 'Take a photo',
                onPressed: busy ? null : onCamera,
                visualDensity: VisualDensity.compact,
                icon: const Icon(Icons.photo_camera_outlined, size: 20),
              ),
              IconButton(
                key: const ValueKey('photo-gallery'),
                tooltip: 'Choose from the gallery',
                onPressed: busy ? null : onGallery,
                visualDensity: VisualDensity.compact,
                icon: const Icon(Icons.photo_library_outlined, size: 20),
              ),
              if (attachment != null)
                IconButton(
                  key: const ValueKey('photo-remove'),
                  tooltip: 'Remove photo',
                  onPressed: busy ? null : onRemove,
                  visualDensity: VisualDensity.compact,
                  icon: const Icon(Icons.close, size: 18),
                ),
            ],
          ),
          if (attachment != null) ...[
            const SizedBox(height: 4),
            Padding(
              padding: const EdgeInsets.only(right: 6),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(10),
                child: Image.file(
                  File(attachment.path),
                  key: const ValueKey('photo-attachment-preview'),
                  width: double.infinity,
                  height: 156,
                  fit: BoxFit.cover,
                  semanticLabel: 'The photo that will be uploaded',
                  errorBuilder: (_, _, _) => Container(
                    width: double.infinity,
                    height: 156,
                    color: scheme.surfaceContainerHigh,
                    alignment: Alignment.center,
                    child: Icon(
                      Icons.image_outlined,
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                ),
              ),
            ),
          ] else ...[
            const SizedBox(height: 2),
            Padding(
              padding: const EdgeInsets.only(right: 6, bottom: 2),
              child: Text(
                'Downsized to 2400 px at quality 85 before upload.',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}
