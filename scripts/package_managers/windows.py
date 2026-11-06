#!/usr/bin/env python3
"""Build self-contained Inno installers from the verified native ZIP archives.

WinGet ZIP Portable aliases change GetModuleFileNameW's location and break the
compiler's existing Bunko lookup. Install the real bin directory on user PATH.
"""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
from release import digest, extract, inventory, immutable, windows_product_code


def installer_script(payload, output, version, flavor, filename):
    # Only generated, safe version/package identifiers reach the Inno template.
    import re
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-pr\.[0-9a-f]{12})?', version):
        raise ValueError('invalid installer version')
    numeric = version.split('-')[0]
    ident = 'Mio' if flavor == 'full' else 'MioCore'
    command = 'mio' if flavor == 'full' else 'mio-core'
    appid = windows_product_code(flavor).removesuffix('_is1').strip('{}')
    return rf'''[Setup]
AppId={{{{{appid}}}
AppName=Mio{' Core' if flavor == 'core' else ''}
UninstallDisplayName=Mio{' Core' if flavor == 'core' else ''}
AppVersion={version}
VersionInfoVersion={numeric}
AppPublisher=Mio Kobo
AppPublisherURL=https://github.com/mio-kobo/mio
DefaultDirName={{localappdata}}\Programs\{ident}
DefaultGroupName={ident}
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
DisableProgramGroupPage=yes
ChangesEnvironment=yes
UninstallDisplayIcon={{app}}\bin\{command}.exe
OutputDir={output}
OutputBaseFilename={filename}
Compression=lzma2
SolidCompression=yes

[Files]
Source: "{payload}\*"; DestDir: "{{app}}"; Flags: ignoreversion recursesubdirs createallsubdirs

[InstallDelete]
Type: filesandordirs; Name: "{{app}}\bunko"
Type: filesandordirs; Name: "{{app}}\examples"

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{{olddata}};{{app}}\bin"; Flags: preservestringtype; Check: NeedsPath

[Code]
function LongPathName(ShortName: String; LongName: String; Size: Cardinal): Cardinal;
  external 'GetLongPathNameW@kernel32.dll stdcall';

function CanonicalPath(Value: String): String;
var Buffer: String; Count: Cardinal;
begin
  Value := Trim(Value);
  if (Length(Value) >= 2) and (Value[1] = '"') and (Value[Length(Value)] = '"') then
    Value := Copy(Value, 2, Length(Value) - 2);
  SetLength(Buffer, 32768);
  Count := LongPathName(Value, Buffer, 32768);
  if (Count > 0) and (Count < 32768) then begin
    SetLength(Buffer, Count);
    Value := Buffer;
  end;
  Result := Uppercase(RemoveBackslashUnlessRoot(Value));
end;

function NeedsPath: Boolean;
var Value, Part, Entry: String; At: Integer; Last: Boolean;
begin
  RegQueryStringValue(HKCU, 'Environment', 'Path', Value);
  Entry := CanonicalPath(ExpandConstant('{{app}}\bin'));
  Result := True;
  repeat
    At := Pos(';', Value);
    Last := At = 0;
    if Last then begin Part := Value; Value := ''; end
    else begin Part := Copy(Value, 1, At - 1); Delete(Value, 1, At); end;
    if (Part <> '') and (CanonicalPath(Part) = Entry) then begin Result := False; Exit; end;
  until Last;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var Value, Updated, Part, Entry: String; At: Integer; Last, First: Boolean;
begin
  {{ Normalize while directories still exist, including 8.3 profile aliases. }}
  if CurUninstallStep = usUninstall then begin
    if RegQueryStringValue(HKCU, 'Environment', 'Path', Value) then begin
      Entry := CanonicalPath(ExpandConstant('{{app}}\bin'));
      Updated := '';
      First := True;
      repeat
        At := Pos(';', Value);
        Last := At = 0;
        if Last then begin Part := Value; Value := ''; end
        else begin Part := Copy(Value, 1, At - 1); Delete(Value, 1, At); end;
        if CanonicalPath(Part) <> Entry then begin
          if not First then Updated := Updated + ';';
          Updated := Updated + Part;
          First := False;
        end;
      until Last;
      if not RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Updated) then
        RaiseException('Cannot remove Mio from user PATH');
    end;
  end;
end;
'''


def build(archive, output, package_version=None):
    compiler = shutil.which('ISCC.exe') or r'C:\Program Files (x86)\Inno Setup 6\ISCC.exe'
    if not Path(compiler).is_file():
        raise RuntimeError('Inno Setup 6 is required on the Windows build host')
    with tempfile.TemporaryDirectory(prefix='mio-inno-') as temp:
        base = Path(temp)
        payload = extract(archive, base / 'unpack')
        m = json.loads((payload / 'manifest.json').read_text())
        assert m['target'] == 'windows-amd64' and inventory(payload) == m['files']
        flavor = m['flavor']
        binary = payload / 'bin/mio.exe'
        original_sha = digest(binary)
        if flavor == 'core':
            binary.rename(payload / 'bin/mio-core.exe')
            m['files'] = inventory(payload)
            (payload / 'manifest.json').write_text(json.dumps(m, sort_keys=True, indent=2)+'\n')
        version = package_version or m['version']
        name = f'mio-{version}-windows-amd64-{flavor}-setup'
        script = base / 'mio.iss'
        script.write_text(installer_script(payload, base, version, flavor, name), encoding='utf-8-sig')
        subprocess.run([compiler, str(script)], check=True)
        output.mkdir(parents=True, exist_ok=True)
        dest = output / (name + '.exe')
        immutable(dest, (base / dest.name).read_bytes())
        assert digest(payload / 'bin' / ('mio-core.exe' if flavor == 'core' else 'mio.exe')) == original_sha
        return dest

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--assets', type=Path, required=True)
    a = p.parse_args()
    lines = []
    for flavor in ('core', 'full'):
        found = list(a.assets.glob(f'mio-*-windows-amd64-{flavor}.zip'))
        if len(found) != 1:
            raise ValueError('one Windows ZIP per flavor required')
        path = build(found[0], a.assets)
        lines.append(f'{digest(path)}  {path.name}\n')
    with (a.assets / 'SHA256SUMS').open('a') as stream:
        stream.write(''.join(lines))
