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
; Tell Explorer the slack:// handlers changed (SHChangeNotify
; SHCNE_ASSOCCHANGED, as Default Programs asks).
ChangesAssociations=yes
; Close a running Clack before replacing its files.
CloseApplications=force
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
; Off by default: the legacy slack key is what Windows runs when it doesn't
; ask, and Slack desktop rewrites it at every start, so taking it silently
; is rude. Clack doesn't need it: the registration below always puts Clack
; in Windows' "Select an app to open this 'slack' link" box, and Clack.exe
; takes the legacy key by itself when no other app has slack://.
Name: "slackurl"; Description: "Make Clack the default for slack:// links (Windows keeps an app you picked with Always)"; GroupDescription: "Links:"; Flags: unchecked
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
; Clack as a slack:// handler Windows can offer, per user (Default
; Programs: an application-specific ProgID, Capabilities with
; URLAssociations, RegisteredApplications; the same keys Slick's installer
; writes for itself). Always written: Windows' "Select an app" box and
; Settings > Default apps list Clack from these. Clack.exe writes the same
; keys at every start (internal/desktop/urlhandler_windows.go).
Root: HKCU; Subkey: "Software\Classes\Clack.slack"; ValueType: string; ValueName: ""; ValueData: "URL:slack Protocol"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Classes\Clack.slack"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""
Root: HKCU; Subkey: "Software\Classes\Clack.slack\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"",0"
Root: HKCU; Subkey: "Software\Classes\Clack.slack\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"" ""%1"""
Root: HKCU; Subkey: "Software\Clack"; Flags: uninsdeletekeyifempty
Root: HKCU; Subkey: "Software\Clack\Capabilities"; ValueType: string; ValueName: "ApplicationName"; ValueData: "Clack"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Clack\Capabilities"; ValueType: string; ValueName: "ApplicationDescription"; ValueData: "Clack is a native Slack client. It signs in with Slack's browser sign-in and opens slack:// links."
Root: HKCU; Subkey: "Software\Clack\Capabilities\URLAssociations"; ValueType: string; ValueName: "slack"; ValueData: "Clack.slack"
Root: HKCU; Subkey: "Software\RegisteredApplications"; ValueType: string; ValueName: "Clack"; ValueData: "Software\Clack\Capabilities"; Flags: uninsdeletevalue
; The box: also the legacy key, what runs when Windows doesn't ask (the
; same as `Clack.exe -register-url-handler`).
Root: HKCU; Subkey: "Software\Classes\slack"; ValueType: string; ValueName: ""; ValueData: "URL:slack Protocol"; Tasks: slackurl
Root: HKCU; Subkey: "Software\Classes\slack"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Tasks: slackurl
Root: HKCU; Subkey: "Software\Classes\slack\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"",0"; Tasks: slackurl
Root: HKCU; Subkey: "Software\Classes\slack\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"" ""%1"""; Tasks: slackurl

[Run]
Filename: "{app}\{#AppExe}"; Description: "{cm:LaunchProgram,{#AppName}}"; Flags: nowait postinstall skipifsilent

[Code]
// On uninstall, besides the keys above: Clack.exe's entry in the slack
// key's OpenWithProgids, and the legacy handler if it still points at this
// Clack. Another app's (Slack desktop's, Slick's) is left alone, and so
// are other apps' OpenWithProgids entries.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Cmd, Mine: String;
begin
  if CurUninstallStep = usUninstall then
  begin
    RegDeleteValue(HKCU, 'Software\Classes\slack\OpenWithProgids', 'Clack.slack');
    RegDeleteKeyIfEmpty(HKCU, 'Software\Classes\slack\OpenWithProgids');
    if RegQueryStringValue(HKCU, 'Software\Classes\slack\shell\open\command', '', Cmd) then
    begin
      Mine := '"' + ExpandConstant('{app}\{#AppExe}') + '"';
      if Pos(Lowercase(Mine), Lowercase(Cmd)) = 1 then
      begin
        RegDeleteKeyIncludingSubkeys(HKCU, 'Software\Classes\slack\shell');
        RegDeleteKeyIncludingSubkeys(HKCU, 'Software\Classes\slack\DefaultIcon');
        RegDeleteValue(HKCU, 'Software\Classes\slack', '');
        RegDeleteValue(HKCU, 'Software\Classes\slack', 'URL Protocol');
        RegDeleteKeyIfEmpty(HKCU, 'Software\Classes\slack');
      end;
    end;
  end;
end;
