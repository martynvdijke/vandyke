import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_exception.dart';
import '../api/models.dart';
import '../providers.dart';
import '../theme/tokens.dart';

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

  @override
  void dispose() {
    _word.dispose();
    _meaning.dispose();
    _person.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    FocusScope.of(context).unfocus();
    if (!(_formKey.currentState?.validate() ?? false)) return;
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      final entry = await ref
          .read(apiClientProvider)
          .createEntry(
            word: _word.text,
            meaning: _meaning.text,
            person: _person.text,
          );
      if (mounted) Navigator.of(context).pop(entry);
    } on ApiException catch (error) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = error.friendlyMessage;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = 'Could not reach the ledger. Check your connection.';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
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
