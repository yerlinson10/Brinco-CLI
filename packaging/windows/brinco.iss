#define MyAppName "Brinco CLI"
#define MyAppPublisher "Brinco"
#define MyAppExeName "brinco.exe"
#define MyAppVersion GetEnv("BRINCO_VERSION")

#if MyAppVersion == ""
  #define MyAppVersion "dev"
#endif

[Setup]
AppId={{A631A316-88E5-42C2-980B-73D1BF12C8A7}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\Brinco
DefaultGroupName=Brinco
DisableProgramGroupPage=yes
OutputDir=Output
OutputBaseFilename=Brinco-Setup-{#MyAppVersion}
Compression=lzma
SolidCompression=yes
WizardStyle=modern
ChangesEnvironment=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "addpath"; Description: "Add Brinco to PATH"; GroupDescription: "Additional tasks:"; Flags: unchecked

[Files]
Source: "..\..\dist\windows-installer\brinco.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Brinco CLI"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\Uninstall Brinco CLI"; Filename: "{uninstallexe}"

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: addpath; Check: NeedsAddPath(ExpandConstant('{app}'))

[Code]
function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;

  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(OrigPath) + ';') = 0;
end;
