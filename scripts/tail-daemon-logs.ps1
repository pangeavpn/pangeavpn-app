# Live-tails the local daemon's log ring via its API. Run with no args to open
# in a new terminal window; -Here tails in the current one.
param(
  [switch]$Here,
  [int]$PollMs = 500,
  [string]$ApiBase = "http://127.0.0.1:8787",
  [string]$TokenPath = "C:\ProgramData\PangeaVPN\daemon-token.txt"
)

$ErrorActionPreference = "Stop"

if (-not $Here) {
  $self = $MyInvocation.MyCommand.Path
  $inner = "-NoExit -ExecutionPolicy Bypass -File `"$self`" -Here -PollMs $PollMs -ApiBase `"$ApiBase`" -TokenPath `"$TokenPath`""
  $wt = Get-Command wt -ErrorAction SilentlyContinue
  if ($wt) {
    Start-Process wt -ArgumentList "new-tab --title `"Pangea daemon logs`" powershell $inner"
  } else {
    Start-Process powershell -ArgumentList $inner
  }
  exit 0
}

$host.UI.RawUI.WindowTitle = "Pangea daemon logs"

function Read-Token {
  if (-not (Test-Path $TokenPath)) {
    throw "daemon token not found at $TokenPath - is the daemon installed?"
  }
  (Get-Content $TokenPath -Raw).Trim()
}

$levelColor = @{ error = "Red"; warn = "Yellow"; info = "Gray"; debug = "DarkGray" }

function Write-Entry($entry) {
  $ts = [DateTimeOffset]::FromUnixTimeMilliseconds($entry.ts).ToLocalTime().ToString("HH:mm:ss.fff")
  $color = $levelColor[[string]$entry.level]
  if (-not $color) { $color = "White" }
  Write-Host "$ts " -NoNewline -ForegroundColor DarkCyan
  Write-Host ("{0,-5} " -f $entry.level) -NoNewline -ForegroundColor $color
  Write-Host ("{0,-9} " -f $entry.source) -NoNewline -ForegroundColor DarkYellow
  Write-Host $entry.msg -ForegroundColor $color
}

$token = Read-Token
$headers = @{ Authorization = "Bearer $token" }
$since = 0
# The API's since filter is inclusive, so entries sharing the last-seen ts
# would repeat; remember what we printed at that ts and skip it.
$printedAtSince = @()
$down = $false

Write-Host "tailing $ApiBase/logs (Ctrl+C to stop)" -ForegroundColor Cyan

while ($true) {
  try {
    $entries = Invoke-RestMethod -Uri "$ApiBase/logs?since=$since" -Headers $headers -TimeoutSec 5
    if ($down) {
      Write-Host "--- daemon is back ---" -ForegroundColor Cyan
      $down = $false
      # A restarted daemon has a fresh token; re-read so auth keeps working.
      $token = Read-Token
      $headers = @{ Authorization = "Bearer $token" }
    }
    foreach ($entry in @($entries)) {
      if ($entry.ts -eq $since -and ($printedAtSince -contains $entry.msg)) { continue }
      Write-Entry $entry
      if ($entry.ts -gt $since) {
        $since = $entry.ts
        $printedAtSince = @($entry.msg)
      } else {
        $printedAtSince += $entry.msg
      }
    }
  } catch {
    if (-not $down) {
      Write-Host "--- daemon unreachable: $($_.Exception.Message) ---" -ForegroundColor DarkRed
      $down = $true
    }
  }
  Start-Sleep -Milliseconds $PollMs
}
