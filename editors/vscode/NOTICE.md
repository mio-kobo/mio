# Notices

## Code license

The Mio Language Support VS Code extension source code is licensed under the
MIT License. See [LICENSE](LICENSE). The MIT license also covers the Mio
compiler source in the parent repository, except where a file or asset states
otherwise.

## Icon and language mark

`icon.png` is a 128×128 raster derivative of the Mio language mark stored at
`editors/jetbrains/src/main/resources/icons/mio.svg` in the parent repository.
`assets/mio.svg` is a copy of that language mark. It is a project logo, not the
Mio/Mochi character illustration, and it does not modify the original SVG.

## Character artwork

The Mio and Mochi character illustrations under `assets/mascot/` in the parent
repository are **not bundled** in this extension. The Mio repository states that
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
