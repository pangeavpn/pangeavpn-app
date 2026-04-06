!macro customInstall
  SetShellVarContext all
  CreateDirectory "$APPDATA\PangeaVPN"
  CreateDirectory "$APPDATA\PangeaVPN\bin"
  CreateDirectory "$APPDATA\PangeaVPN\bin\win"

  nsExec::ExecToLog 'sc.exe stop PangeaDaemon'
  Sleep 500
  nsExec::ExecToLog 'sc.exe delete PangeaDaemon'
  Sleep 500

  CopyFiles /SILENT "$INSTDIR\resources\daemon\PangeaDaemon.exe" "$APPDATA\PangeaVPN\PangeaDaemon.exe"
  CopyFiles /SILENT "$INSTDIR\resources\daemon\wireguard.dll" "$APPDATA\PangeaVPN\wireguard.dll"
  CopyFiles /SILENT "$INSTDIR\resources\daemon\wintun.dll" "$APPDATA\PangeaVPN\wintun.dll"
  CopyFiles /SILENT "$INSTDIR\resources\bin\win\*.*" "$APPDATA\PangeaVPN\bin\win"

  nsExec::ExecToLog 'sc.exe create PangeaDaemon binPath= "\"$APPDATA\PangeaVPN\PangeaDaemon.exe\" --service" start= auto obj= LocalSystem DisplayName= "Pangea VPN Daemon"'
  nsExec::ExecToLog 'sc.exe description PangeaDaemon "Pangea VPN privileged daemon service"'
  nsExec::ExecToLog 'sc.exe failure PangeaDaemon reset= 86400 actions= restart/5000/restart/5000/restart/5000'
  nsExec::ExecToLog 'sc.exe failureflag PangeaDaemon 1'
  nsExec::ExecToLog 'powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$$ErrorActionPreference = \"Stop\"; $$serviceName = \"PangeaDaemon\"; $$ace = \"(A;;RPLOLC;;;BU)\"; $$sd = (sc.exe sdshow $$serviceName | Out-String).Trim(); if ([string]::IsNullOrWhiteSpace($$sd)) { exit 1 }; if ($$sd -notlike \"*$$ace*\") { $$sIndex = $$sd.IndexOf(\"S:\"); if ($$sIndex -ge 0) { $$sd = $$sd.Substring(0, $$sIndex) + $$ace + $$sd.Substring($$sIndex) } else { $$sd = $$sd + $$ace }; sc.exe sdset $$serviceName $$sd | Out-Null; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE } }"'
  nsExec::ExecToLog 'sc.exe start PangeaDaemon'
!macroend

!macro customUnInstall
  SetShellVarContext all
  nsExec::ExecToLog 'sc.exe stop PangeaDaemon'
  Sleep 500
  nsExec::ExecToLog 'sc.exe delete PangeaDaemon'

  Delete "$APPDATA\PangeaVPN\bin\win\*.*"
  RMDir "$APPDATA\PangeaVPN\bin\win"
  RMDir "$APPDATA\PangeaVPN\bin"
  Delete "$APPDATA\PangeaVPN\wireguard.dll"
  Delete "$APPDATA\PangeaVPN\wintun.dll"
  Delete "$APPDATA\PangeaVPN\PangeaDaemon.exe"
!macroend
