# Publishing notes

This document is a checklist for a future Visual Studio Marketplace release.
The MVP has **not** been published.

## Publisher ID

`package.json` currently uses:

```json
"publisher": "exmikupro"
```

This value is the existing GitHub organization name from the Mio repository. It
is a placeholder, not a confirmed Marketplace publisher ID. Before publishing:

1. Create or select the official Visual Studio Marketplace publisher.
2. Replace `publisher` with the exact publisher ID.
3. Run `npx vsce login <publisher>`.
4. Re-run `npm test`, `npx vsce package`, and `npm run check:vsix`.

Do not publish under an invented or unverified publisher ID.

## Pre-publish checklist

- [ ] Replace the placeholder `publisher` value.
- [ ] Confirm the extension name `mio-language-support` and display name
      `Mio — Programming Language Tools`.
- [ ] Confirm the version in `package.json`, `CHANGELOG.md`, and the VSIX file
      name agree.
- [ ] Confirm the MIT license in `LICENSE` and the artwork note in `NOTICE.md`.
- [ ] Do not bundle the Mio/Mochi character illustrations unless separate
      permission has been obtained.
- [ ] Verify `icon.png` is a 128×128 PNG.
- [ ] Verify the README images and links render on the Marketplace page.
- [ ] Run:

  ```bash
  npm ci
  npm run compile
  npm run lint
  npm test
  npx vsce package
  npm run check:vsix
  ```

- [ ] Install the VSIX locally on macOS, Windows, and Linux and repeat the
      manual checks below.
- [ ] Check that `mio.executablePath` with spaces and non-ASCII characters works
      on each platform.
- [ ] Confirm no secrets, local absolute paths, `.test-tmp`, `node_modules`, or
      test files are packaged.
- [ ] Publish only after the project owner explicitly approves the release.

## Manual smoke test

1. Install the VSIX.
2. Open a `.mio` file and verify the language mode is **Mio**.
3. Verify keywords, types, strings, runes, comments, generics, and functions
   highlight correctly in a dark and a light theme.
4. Type `(` and `"` in a Mio file and verify auto-closing.
5. Type a complete `moshi` block and verify indentation.
6. Insert snippets from the command palette or IntelliSense.
7. With no Mio CLI configured, run **Mio: Check Current File** and verify the
   guidance message appears without breaking highlighting.
8. Configure a Mio CLI path containing spaces and non-ASCII characters.
9. Run **Mio: Check Current File** on a file with a type error and verify the
   Problems entry.
10. Run **Mio: Show Compiler Version**, **Mio: Run Current File**, and
    **Mio: Build Current File**.
11. Close the file and verify its diagnostics disappear.
12. Reload the window and verify no Mio processes remain.

## Marketplace description

Suggested short description:

> Mio programming language support: syntax highlighting, snippets, compiler
> diagnostics, and CLI integration.

Suggested categories:

- Programming Languages
- Linters

Suggested keywords:

- mio
- mochi
- bunko
- language
- syntax-highlighting
- compiler
- diagnostics
