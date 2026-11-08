import 'package:flutter/material.dart';

import 'tokens.dart';

/// Editorial type and surfaces for the pocket ledger.
abstract final class VandykeTheme {
  static ThemeData light() => _build(
    brightness: Brightness.light,
    background: VandykeColors.parchment,
    surface: VandykeColors.surface,
    surfaceAlt: VandykeColors.surfaceAlt,
    ink: VandykeColors.ink,
    muted: VandykeColors.muted,
    line: VandykeColors.line,
    primary: VandykeColors.green,
    onPrimary: Colors.white,
    accent: VandykeColors.brass,
  );

  static ThemeData dark() => _build(
    brightness: Brightness.dark,
    background: VandykeColors.night,
    surface: VandykeColors.nightSurface,
    surfaceAlt: VandykeColors.nightAlt,
    ink: VandykeColors.cream,
    muted: VandykeColors.nightMuted,
    line: VandykeColors.nightLine,
    primary: VandykeColors.mint,
    onPrimary: VandykeColors.night,
    accent: VandykeColors.paleBrass,
  );

  static ThemeData _build({
    required Brightness brightness,
    required Color background,
    required Color surface,
    required Color surfaceAlt,
    required Color ink,
    required Color muted,
    required Color line,
    required Color primary,
    required Color onPrimary,
    required Color accent,
  }) {
    final scheme = ColorScheme(
      brightness: brightness,
      primary: primary,
      onPrimary: onPrimary,
      primaryContainer: primary.withValues(
        alpha: brightness == Brightness.light ? 0.12 : 0.18,
      ),
      onPrimaryContainer: ink,
      secondary: accent,
      onSecondary: brightness == Brightness.light
          ? Colors.white
          : VandykeColors.night,
      secondaryContainer: accent.withValues(alpha: 0.14),
      onSecondaryContainer: ink,
      tertiary: accent,
      onTertiary: brightness == Brightness.light
          ? Colors.white
          : VandykeColors.night,
      error: VandykeColors.danger,
      onError: Colors.white,
      surface: surface,
      onSurface: ink,
      surfaceContainerLowest: background,
      surfaceContainerLow: surfaceAlt,
      surfaceContainer: surface,
      surfaceContainerHigh: surfaceAlt,
      surfaceContainerHighest: surfaceAlt,
      onSurfaceVariant: muted,
      outline: line,
      outlineVariant: line.withValues(alpha: 0.6),
      shadow: Colors.black,
      scrim: Colors.black,
      inverseSurface: ink,
      onInverseSurface: background,
      inversePrimary: primary,
    );

    final base = ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      scaffoldBackgroundColor: background,
      fontFamily: 'DMSans',
      visualDensity: VisualDensity.standard,
    );

    final serif = base.textTheme.apply(
      fontFamily: 'Newsreader',
      bodyColor: ink,
      displayColor: ink,
    );
    final body = base.textTheme.apply(
      fontFamily: 'DMSans',
      bodyColor: ink,
      displayColor: ink,
    );

    return base.copyWith(
      textTheme: body.copyWith(
        displayLarge: serif.displayLarge?.copyWith(
          fontSize: 46,
          height: 0.98,
          letterSpacing: -1.8,
          fontWeight: FontWeight.w500,
        ),
        displayMedium: serif.displayMedium?.copyWith(
          fontSize: 38,
          height: 1,
          letterSpacing: -1.4,
          fontWeight: FontWeight.w500,
        ),
        displaySmall: serif.displaySmall?.copyWith(
          fontSize: 31,
          height: 1.05,
          letterSpacing: -1,
          fontWeight: FontWeight.w500,
        ),
        headlineLarge: serif.headlineLarge?.copyWith(
          fontSize: 27,
          height: 1.1,
          letterSpacing: -0.6,
          fontWeight: FontWeight.w600,
        ),
        headlineMedium: serif.headlineMedium?.copyWith(
          fontSize: 22,
          height: 1.15,
          letterSpacing: -0.3,
          fontWeight: FontWeight.w600,
        ),
        headlineSmall: serif.headlineSmall?.copyWith(
          fontSize: 19,
          height: 1.2,
          fontWeight: FontWeight.w600,
        ),
        titleLarge: body.titleLarge?.copyWith(
          fontSize: 18,
          fontWeight: FontWeight.w600,
          letterSpacing: -0.2,
        ),
        titleMedium: body.titleMedium?.copyWith(
          fontSize: 15,
          fontWeight: FontWeight.w600,
        ),
        bodyLarge: body.bodyLarge?.copyWith(fontSize: 16, height: 1.5),
        bodyMedium: body.bodyMedium?.copyWith(fontSize: 14, height: 1.45),
        labelLarge: body.labelLarge?.copyWith(
          fontSize: 13,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.2,
        ),
        labelSmall: body.labelSmall?.copyWith(
          fontSize: 10,
          fontWeight: FontWeight.w700,
          letterSpacing: 1.4,
        ),
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: background,
        surfaceTintColor: Colors.transparent,
        foregroundColor: ink,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: serif.headlineSmall?.copyWith(
          fontSize: 22,
          fontWeight: FontWeight.w600,
          color: ink,
        ),
      ),
      cardTheme: CardThemeData(
        color: surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(18),
          side: BorderSide(color: line.withValues(alpha: 0.7)),
        ),
      ),
      dividerTheme: DividerThemeData(color: line, thickness: 1, space: 1),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: surfaceAlt,
        hintStyle: body.bodyMedium?.copyWith(color: muted),
        labelStyle: body.bodyMedium?.copyWith(color: muted),
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 15,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(color: line),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(color: line),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(color: primary, width: 1.5),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: const BorderSide(color: VandykeColors.danger),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: primary,
          foregroundColor: onPrimary,
          minimumSize: const Size(0, 52),
          padding: const EdgeInsets.symmetric(horizontal: 24),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(12),
          ),
          textStyle: body.labelLarge?.copyWith(
            fontSize: 14,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: ink,
          minimumSize: const Size(0, 48),
          side: BorderSide(color: line),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(12),
          ),
          textStyle: body.labelLarge,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: primary,
          textStyle: body.labelLarge,
        ),
      ),
      chipTheme: ChipThemeData(
        backgroundColor: surfaceAlt,
        selectedColor: primary.withValues(alpha: 0.14),
        side: BorderSide(color: line),
        labelStyle: body.bodyMedium?.copyWith(fontWeight: FontWeight.w500),
        secondaryLabelStyle: body.bodyMedium?.copyWith(
          fontWeight: FontWeight.w600,
          color: primary,
        ),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(999)),
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: surface,
        surfaceTintColor: Colors.transparent,
        indicatorColor: primary.withValues(alpha: 0.14),
        height: 72,
        labelTextStyle: WidgetStateProperty.resolveWith((states) {
          final selected = states.contains(WidgetState.selected);
          return body.labelSmall?.copyWith(
            fontSize: 10,
            letterSpacing: 0.5,
            color: selected ? primary : muted,
            fontWeight: FontWeight.w600,
          );
        }),
        iconTheme: WidgetStateProperty.resolveWith((states) {
          final selected = states.contains(WidgetState.selected);
          return IconThemeData(color: selected ? primary : muted, size: 23);
        }),
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: ink,
        contentTextStyle: body.bodyMedium?.copyWith(color: background),
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
      progressIndicatorTheme: ProgressIndicatorThemeData(color: primary),
      listTileTheme: ListTileThemeData(iconColor: muted, textColor: ink),
      pageTransitionsTheme: const PageTransitionsTheme(
        builders: {
          TargetPlatform.android: FadeForwardsPageTransitionsBuilder(),
          TargetPlatform.iOS: FadeUpwardsPageTransitionsBuilder(),
        },
      ),
    );
  }
}
