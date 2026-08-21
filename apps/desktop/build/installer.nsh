; PangeaVPN Windows installer customizations.
;
; Assisted (branded) NSIS wizard. Adds:
;   - an Options page with a "create desktop shortcut" checkbox,
;   - a localized "what's new" link on the Finish page,
;   - registration of the privileged PangeaDaemon Windows service.
;
; electron-builder inserts customHeader AFTER its finish page is emitted, so the
; shared vars/functions and the finish-page defines live in customPageAfterChangeDir
; instead — that hook runs before MUI_PAGE_FINISH, letting electron-builder's own
; finish page (with its "run after finish" checkbox) also pick up our "what's new"
; link. Custom UI strings are localized at runtime from the active installer
; language ($LANGUAGE) rather than via LangString, so this file compiles regardless
; of insertion order. Standard wizard chrome is localized by NSIS's language files.
; Non-English strings are machine translations pending native review.

!macro customPageAfterChangeDir
  !include "nsDialogs.nsh"
  !include "LogicLib.nsh"

  Var PangeaDesktopShortcut   ; "1" = create desktop shortcut, "0" = skip
  Var PangeaChkDesktop        ; nsDialogs checkbox handle
  Var PangeaOptTitle
  Var PangeaOptSubtitle
  Var PangeaOptDesktop
  Var PangeaWhatsNew

  Function PangeaOpenReleaseNotes
    ExecShell "open" "https://github.com/pangeavpn/pangeavpn-app/releases/latest"
  FunctionEnd

  ; Populate the custom strings for the active installer language ($LANGUAGE).
  Function PangeaResolveStrings
    ${Switch} $LANGUAGE
      ${Case} 1034 ; Spanish
        StrCpy $PangeaOptTitle "Opciones"
        StrCpy $PangeaOptSubtitle "Elija tareas adicionales"
        StrCpy $PangeaOptDesktop "Crear un acceso directo en el escritorio"
        StrCpy $PangeaWhatsNew "Ver novedades"
        ${Break}
      ${Case} 1036 ; French
        StrCpy $PangeaOptTitle "Options"
        StrCpy $PangeaOptSubtitle "Choisir des tâches supplémentaires"
        StrCpy $PangeaOptDesktop "Créer un raccourci sur le bureau"
        StrCpy $PangeaWhatsNew "Voir les nouveautés"
        ${Break}
      ${Case} 1049 ; Russian
        StrCpy $PangeaOptTitle "Параметры"
        StrCpy $PangeaOptSubtitle "Выберите дополнительные задачи"
        StrCpy $PangeaOptDesktop "Создать ярлык на рабочем столе"
        StrCpy $PangeaWhatsNew "Что нового"
        ${Break}
      ${Case} 1058 ; Ukrainian
        StrCpy $PangeaOptTitle "Параметри"
        StrCpy $PangeaOptSubtitle "Виберіть додаткові завдання"
        StrCpy $PangeaOptDesktop "Створити ярлик на робочому столі"
        StrCpy $PangeaWhatsNew "Що нового"
        ${Break}
      ${Case} 2052 ; Simplified Chinese
        StrCpy $PangeaOptTitle "选项"
        StrCpy $PangeaOptSubtitle "选择附加任务"
        StrCpy $PangeaOptDesktop "创建桌面快捷方式"
        StrCpy $PangeaWhatsNew "查看新增功能"
        ${Break}
      ${Case} 1025 ; Arabic
        StrCpy $PangeaOptTitle "خيارات"
        StrCpy $PangeaOptSubtitle "اختر مهام إضافية"
        StrCpy $PangeaOptDesktop "إنشاء اختصار على سطح المكتب"
        StrCpy $PangeaWhatsNew "عرض الجديد"
        ${Break}
      ${Default} ; English and fallback
        StrCpy $PangeaOptTitle "Options"
        StrCpy $PangeaOptSubtitle "Choose additional tasks"
        StrCpy $PangeaOptDesktop "Create a desktop shortcut"
        StrCpy $PangeaWhatsNew "View what's new"
        ${Break}
    ${EndSwitch}
  FunctionEnd

  ; Options wizard page — a single "create desktop shortcut" checkbox.
  Function PangeaOptionsPageShow
    Call PangeaResolveStrings
    !insertmacro MUI_HEADER_TEXT "$PangeaOptTitle" "$PangeaOptSubtitle"
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
      Abort
    ${EndIf}
    ${NSD_CreateCheckbox} 0 10u 100% 12u "$PangeaOptDesktop"
    Pop $PangeaChkDesktop
    ${If} $PangeaDesktopShortcut != "0"
      ${NSD_Check} $PangeaChkDesktop
    ${EndIf}
    nsDialogs::Show
  FunctionEnd

  Function PangeaOptionsPageLeave
    ${NSD_GetState} $PangeaChkDesktop $0
    ${If} $0 == 1
      StrCpy $PangeaDesktopShortcut "1"
    ${Else}
      StrCpy $PangeaDesktopShortcut "0"
    ${EndIf}
  FunctionEnd

  ; Finish-page "what's new" link. Defined here (before electron-builder's
  ; MUI_PAGE_FINISH) so it is added alongside the built-in "run after finish"
  ; checkbox. Unchecked by default so it reads as an optional link.
  !define MUI_FINISHPAGE_SHOWREADME ""
  !define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
  !define MUI_FINISHPAGE_SHOWREADME_TEXT "$PangeaWhatsNew"
  !define MUI_FINISHPAGE_SHOWREADME_FUNCTION "PangeaOpenReleaseNotes"

  Page custom PangeaOptionsPageShow PangeaOptionsPageLeave
!macroend

; Default the shortcut choice to "create" so silent installs and silent
; auto-updates (which skip the Options page under /S) still get the shortcut.
!macro customInit
  StrCpy $PangeaDesktopShortcut "1"
  Call PangeaResolveStrings
!macroend

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

  ; Name and icon Windows shows on our toasts; without it they are attributed
  ; to the Electron runtime instead of PangeaVPN.
  WriteRegStr HKLM "Software\Classes\AppUserModelId\${APP_ID}" "DisplayName" "${PRODUCT_NAME}"
  WriteRegStr HKLM "Software\Classes\AppUserModelId\${APP_ID}" "IconUri" "$INSTDIR\resources\build\PangeaVPN.png"

  ; Optional desktop shortcut (per the Options page; created on the common
  ; desktop since this is a per-machine install). Non-fatal if it fails.
  ${If} $PangeaDesktopShortcut == "1"
    CreateShortcut "$DESKTOP\${PRODUCT_FILENAME}.lnk" "$INSTDIR\${APP_EXECUTABLE_FILENAME}" "" "$INSTDIR\${APP_EXECUTABLE_FILENAME}" 0
  ${EndIf}
!macroend

!macro customUnInstall
  SetShellVarContext all
  nsExec::ExecToLog 'sc.exe stop PangeaDaemon'
  Sleep 500
  nsExec::ExecToLog 'sc.exe delete PangeaDaemon'

  DeleteRegKey HKLM "Software\Classes\AppUserModelId\${APP_ID}"

  Delete "$DESKTOP\${PRODUCT_FILENAME}.lnk"

  Delete "$APPDATA\PangeaVPN\bin\win\*.*"
  RMDir "$APPDATA\PangeaVPN\bin\win"
  RMDir "$APPDATA\PangeaVPN\bin"
  Delete "$APPDATA\PangeaVPN\wireguard.dll"
  Delete "$APPDATA\PangeaVPN\wintun.dll"
  Delete "$APPDATA\PangeaVPN\PangeaDaemon.exe"
!macroend
