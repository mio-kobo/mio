# Notices

## Code license

The Mio — Programming Language Tools VS Code extension source code is licensed under the
MIT License. See [LICENSE](LICENSE). The MIT license also covers the Mio
compiler source in the parent repository, except where a file or asset states
otherwise.

## Icon and language mark

`icon.png` is a 128×128 RGBA raster conversion of the JetBrains Marketplace icon
stored at
`editors/jetbrains/src/main/resources/META-INF/pluginIcon.svg` in the parent
repository. It uses the PNG artwork embedded in that SVG without redrawing or
replacing it, and preserves the original transparent background, colors,
proportions, and 5% margin. `assets/mio.svg` is the separate Mio language mark
used for file-type icons; it is not the Marketplace icon.

## Character artwork

The Marketplace icon artwork is embedded in the parent repository's
`pluginIcon.svg`. Other Mio and Mochi character illustrations under
`assets/mascot/` in the parent repository are **not bundled** in this
extension. The Mio repository states that
the character artwork is licensed separately from the MIT-licensed code. Do not
redistribute or modify those illustrations without the appropriate permission.
The README links to the project instead of embedding the artwork.

## Development-only dependencies

The following packages are used only for development and testing and are not
included in the packaged VSIX:

- `adm-zip` (MIT)
- `@vscode/test-electron` (MIT)
- `@vscode/vsce` (MIT)
- `@typescript-eslint/*` (MIT)
- `eslint` (MIT)
- `globals` (MIT)
- `mocha` (MIT)
- `typescript` (Apache-2.0)
- `vscode-oniguruma` (MIT)
- `vscode-textmate` (MIT)

The generated preview images under `docs/images/` are documentation assets
created by `scripts/generate-assets.py`. They use the scope colors of the
extension and do not contain third-party artwork.
