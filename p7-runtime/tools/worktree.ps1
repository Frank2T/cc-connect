param(
  [Parameter(Mandatory=$true)][ValidateSet('create','remove','list','path')][string]$Action,
  [string]$Name,
  [string]$Repo = 'E:\CodexTelegram\.ccsrc',
  [string]$Root = 'E:\CodexTelegram\.worktrees'
)
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path $Root | Out-Null
switch ($Action) {
  'create' {
    if ([string]::IsNullOrWhiteSpace($Name)) { throw 'Name required' }
    $target = Join-Path $Root $Name
    git -C $Repo worktree add -b "codex/$Name" $target HEAD
    Write-Output $target
  }
  'remove' {
    if ([string]::IsNullOrWhiteSpace($Name)) { throw 'Name required' }
    $target = Join-Path $Root $Name
    git -C $Repo worktree remove --force $target
  }
  'list' { git -C $Repo worktree list }
  'path' {
    if ([string]::IsNullOrWhiteSpace($Name)) { throw 'Name required' }
    Write-Output (Join-Path $Root $Name)
  }
}

