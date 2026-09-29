; Inno Setup script SKETCH for a Mediarium Windows installer.
;
; STATUS: UNTESTED. This file has never been compiled. Inno Setup (winget id
; JRSoftware.InnoSetup) was not run. Treat it as a starting point and expect
; to fix small syntax or path problems on first compile.
;
; What it does:
;   * installs mediarium.exe, LICENSE, README-INSTALL.txt, run-mediarium.ps1 and
;     docs\INSTALL.md to Program Files\Mediarium
;   * adds a Start menu entry that runs run-mediarium.ps1 (checks for 7z/par2,
;     sets the folder variables, starts the app) and one that opens the web UI
;   * OPTIONAL task "Start Mediarium when I sign in": registers a Windows
;     Scheduled Task (built in to Windows, no extra tool) that runs the same
;     script at logon.
;
; What it deliberately does NOT do:
;   * download or install 7-Zip or par2. Installers that fetch other programs
;     silently are not something to ship by default. The script and INSTALL.md
;     tell the user what to install.
;   * install a real Windows service. mediarium.exe does not implement the
;     Windows service control protocol (nothing in cmd/app/ does), so it cannot
;     be registered as a service directly. A service wrapper is needed. NSSM
;     (winget id NSSM.NSSM, https://nssm.cc) is the usual one; example commands
;     are in the comment block at the bottom. NSSM is a separate download with
;     its own license and is not bundled here.
;
; Build (on Windows, after unpacking the release zip next to this file, or
; adjust SourceDir):   iscc mediarium.iss
; Expected layout at build time (all paths relative to this .iss file):
;   ..\..\dist\mediarium.exe         (the built binary, name it exactly that)
;   ..\..\LICENSE
;   ..\README-INSTALL.txt
;   run-mediarium.ps1
;   ..\..\docs\INSTALL.md
; AppVersion is passed on the command line:  iscc /DAppVersion=0.1.0 mediarium.iss

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif

[Setup]
AppId={{B7A1B0E2-5C1D-4C8E-9C57-4D0E5D3B2A11}
AppName=Mediarium
AppVersion={#AppVersion}
AppPublisher=Mediarium contributors
AppPublisherURL=https://github.com/rdborg/mediarium
DefaultDirName={autopf}\Mediarium
DefaultGroupName=Mediarium
DisableProgramGroupPage=yes
LicenseFile=..\..\LICENSE
OutputDir=..\..\dist
OutputBaseFilename=mediarium-setup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
; 64-bit only: the release pipeline builds windows/amd64 only.
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
WizardStyle=modern

[Tasks]
Name: "autostart"; Description: "Start Mediarium when I sign in to Windows (Scheduled Task)"; Flags: unchecked

[Files]
Source: "..\..\dist\mediarium.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README-INSTALL.txt"; DestDir: "{app}"; Flags: ignoreversion
Source: "run-mediarium.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\docs\INSTALL.md"; DestDir: "{app}\docs"; Flags: ignoreversion

[Icons]
Name: "{group}\Mediarium (start)"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; \
  Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\run-mediarium.ps1"""; \
  WorkingDir: "{app}"; Comment: "Starts Mediarium and keeps this window open (close it to stop)"
Name: "{group}\Mediarium web interface"; Filename: "http://localhost:8264"
Name: "{group}\Install notes"; Filename: "{app}\README-INSTALL.txt"
Name: "{group}\Uninstall Mediarium"; Filename: "{uninstallexe}"

[Run]
; Optional: register a per-user Scheduled Task that starts Mediarium hidden at
; logon. schtasks.exe ships with Windows. The task runs as the installing
; user's interactive account (not as SYSTEM) so file ownership and the default
; data folder (%USERPROFILE%\Mediarium) behave like a normal run.
; NOTE: with PrivilegesRequired=admin, {username} inside [Run] is the account
; that elevated the installer, which may differ from the signed-in user on a
; machine where a different admin approved the UAC prompt. UNTESTED.
Filename: "{sys}\schtasks.exe"; \
  Parameters: "/Create /F /TN ""Mediarium"" /SC ONLOGON /RL LIMITED /TR ""powershell.exe -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File \""{app}\run-mediarium.ps1\"""""; \
  Flags: runhidden; Tasks: autostart

[UninstallRun]
Filename: "{sys}\schtasks.exe"; Parameters: "/Delete /F /TN ""Mediarium"""; Flags: runhidden; RunOnceId: "RemoveMediariumTask"

; -----------------------------------------------------------------------------
; Alternative: run as a real Windows service with NSSM (UNTESTED). After
; installing NSSM (winget install NSSM.NSSM), from an elevated prompt:
;
;   nssm install Mediarium "C:\Program Files\Mediarium\mediarium.exe"
;   nssm set Mediarium AppDirectory "C:\Program Files\Mediarium"
;   nssm set Mediarium AppEnvironmentExtra CONFIG_DIR=D:\Media\config DOWNLOADS_DIR=D:\Media\downloads MOVIES_DIR=D:\Media\movies TV_DIR=D:\Media\tv APP_PORT=8264
;   nssm set Mediarium Start SERVICE_AUTO_START
;   nssm start Mediarium
;
; A service runs as LocalSystem by default. Give it an account (nssm set
; Mediarium ObjectName ...) that can write to your media folders, and make sure
; 7z.exe and par2.exe are on the SYSTEM-wide PATH (services do not read a
; per-user PATH).
; -----------------------------------------------------------------------------
