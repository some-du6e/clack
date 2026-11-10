; Inno Setup script for the Clack installer (per-user, no admin rights).
;
; Built by .github/workflows/release.yml:
;   iscc /DAppVersion=0.3.1 /DAppVersionNum=0.3.1.0 /DSourceExe=..\..\dist\Clack.exe /DOutputDir=..\..\dist packaging\windows\clack.iss
; Paths are relative to this file.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef AppVersionNum
  #define AppVersionNum "0.0.0.0"
#endif
#ifndef SourceExe
  #define SourceExe "..\..\dist\windows-amd64\Clack.exe"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif

#define AppName "Clack"
#define AppExe "Clack.exe"
#define AppURL "https://github.com/some-du6e/clack"

[Setup]
; Never change AppId: upgrades and the uninstaller find the existing install by it.
AppId={{71A683CE-53A9-4BFE-877D-F9A6A1CA70FA}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=Clack contributors
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/issues
AppUpdatesURL={#AppURL}/releases
VersionInfoVersion={#AppVersionNum}
VersionInfoProductName={#AppName}
VersionInfoProductTextVersion={#AppVersion}
VersionInfoDescription={#AppName} Setup
VersionInfoCompany=Clack contributors
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
DisableDirPage=auto
DisableWelcomePage=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
SetupIconFile=..\..\assets\icon\clack.ico
UninstallDisplayIcon={app}\{#AppExe}
UninstallDisplayName={#AppName}
OutputDir={#OutputDir}
OutputBaseFilename=Clack-{#AppVersion}-windows-amd64-setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; Tell Explorer the slack:// handler changed.
ChangesAssociations=yes
; Close a running Clack before replacing its files.
CloseApplications=force
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
; Off by default: the Slack desktop app also owns slack:// (it re-registers
; on every start), and taking the scheme from it silently is rude. Clack only
; needs it to receive sign-in links straight from the browser; copying the
; link into Clack's sign-in screen works without it (README "Sign in").
Name: "slackurl"; Description: "Open slack:// links with Clack (the Slack desktop app also claims these)"; GroupDescription: "Links:"; Flags: unchecked
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "{#AppExe}"; Flags: ignoreversion
Source: "README.txt"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion
Source: "..\..\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; Comment: "Slack client"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon

[Registry]
; Same keys as `Clack.exe -register-url-handler`.
Root: HKCU; Subkey: "Software\Classes\slack"; ValueType: string; ValueName: ""; ValueData: "URL:slack Protocol"; Tasks: slackurl
Root: HKCU; Subkey: "Software\Classes\slack"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Tasks: slackurl
Root: HKCU; Subkey: "Software\Classes\slack\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"",0"; Tasks: slackurl
Root: HKCU; Subkey: "Software\Classes\slack\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"" ""%1"""; Tasks: slackurl

[Run]
Filename: "{app}\{#AppExe}"; Description: "{cm:LaunchProgram,{#AppName}}"; Flags: nowait postinstall skipifsilent

[Code]
// Remove the slack:// handler on uninstall, but only if it still points at
// this Clack. If another app (e.g. the official Slack client) took it over
// since, leave it alone.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Cmd, Mine: String;
begin
  if CurUninstallStep = usUninstall then
  begin
    if RegQueryStringValue(HKCU, 'Software\Classes\slack\shell\open\command', '', Cmd) then
    begin
      Mine := '"' + ExpandConstant('{app}\{#AppExe}') + '"';
      if Pos(Lowercase(Mine), Lowercase(Cmd)) = 1 then
        RegDeleteKeyIncludingSubkeys(HKCU, 'Software\Classes\slack');
    end;
  end;
end;
