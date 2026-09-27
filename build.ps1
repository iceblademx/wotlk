<#
.SYNOPSIS
  Native Windows build script for wowsims-cc (WotLK 3.3.5a fork of wowsims/wotlk).
  Mirrors the targets of the upstream `makefile` without needing make/bash.

.EXAMPLE
  ./build.ps1 setup        # one-time: npm install + protoc-gen-go
  ./build.ps1              # full client build into dist/wotlk (same as `dist`)
  ./build.ps1 host         # build + serve at http://localhost:8080/wotlk/
  ./build.ps1 devserver    # build wowsimwotlk.exe (native sim server, UI embedded)
  ./build.ps1 rundevserver # build + run native server using dist/ at http://localhost:3333/wotlk/
  ./build.ps1 test         # go test --tags=with_db ./sim/...
  ./build.ps1 update-tests # accept *.results.tmp as new expected results
  ./build.ps1 items        # regenerate assets/database/db.{bin,json} (includes imported custom content)
  ./build.ps1 cc           # import cc_data/ (DBCs + server data), then regenerate the item DB
  ./build.ps1 inspect spell 12345   # decode a spell/item/set/enchant from cc_data/ (see tools/cc/inspect)
  ./build.ps1 dump         # export server tables (item_template, spell_proc, ...) via .env credentials (read-only)
  ./build.ps1 audit        # compare hardcoded item effect values with 3.3.5a spell data (add -Fix via: ./build.ps1 audit -fix)
  ./build.ps1 wasm | proto | ui | fmt | clean
#>
param(
	[Parameter(Position = 0)]
	[ValidateSet('dist', 'setup', 'proto', 'wasm', 'ui', 'host', 'devserver', 'rundevserver', 'release', 'test', 'update-tests', 'items', 'cc', 'inspect', 'dump', 'audit', 'fmt', 'clean')]
	[string]$Target = 'dist',
	[Parameter(Position = 1, ValueFromRemainingArguments = $true)]
	[string[]]$Rest = @(),
	[int]$Port = 8080
)

$ErrorActionPreference = 'Stop'
$Root = $PSScriptRoot
Set-Location $Root

$OutDir = Join-Path $Root 'dist/wotlk'
$SiteTitle = 'WotLK 3.3.5a'

# ---------------------------------------------------------------------------
# Toolchain: prefer the portable tools in .tools/ when present.
# ---------------------------------------------------------------------------
$tools = Join-Path $Root '.tools'
foreach ($p in @('go/bin', 'gobin', 'protoc/bin', 'git/cmd')) {
	$full = Join-Path $tools $p
	if ((Test-Path $full) -and -not ($env:PATH -split ';' -contains $full)) {
		$env:PATH = "$full;$env:PATH"
	}
}
if (Test-Path (Join-Path $tools 'gobin')) { $env:GOBIN = Join-Path $tools 'gobin' }

function Invoke-Native {
	param([string]$Exe, [string[]]$Arguments)
	& $Exe @Arguments
	if ($LASTEXITCODE -ne 0) { throw "$Exe $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
}

function Write-Step([string]$msg) { Write-Host "==> $msg" -ForegroundColor Cyan }

function Test-Stale {
	# True when $Output is missing or older than any file in $Inputs.
	param([string]$Output, [string[]]$Inputs)
	if (-not (Test-Path $Output)) { return $true }
	$outTime = (Get-Item $Output).LastWriteTimeUtc
	foreach ($i in $Inputs) {
		if (-not (Test-Path $i)) { continue }
		$newer = Get-ChildItem $i -Recurse -File -ErrorAction SilentlyContinue |
			Where-Object { $_.LastWriteTimeUtc -gt $outTime } | Select-Object -First 1
		if ($newer) { return $true }
	}
	return $false
}

# ---------------------------------------------------------------------------
# Targets
# ---------------------------------------------------------------------------
function Invoke-Setup {
	Write-Step 'npm install'
	Invoke-Native 'npm' @('install', '--no-audit', '--no-fund')
	if (-not (Get-Command protoc-gen-go -ErrorAction SilentlyContinue)) {
		Write-Step 'Installing protoc-gen-go'
		Invoke-Native 'go' @('install', 'google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6')
	}
	if (Test-Path (Join-Path $Root '.git')) {
		Copy-Item (Join-Path $Root 'pre-commit') (Join-Path $Root '.git/hooks/pre-commit') -Force
	}
}

function Invoke-Proto {
	# Relative paths: protoc requires them to share the -I prefix exactly.
	$protoFiles = (Get-ChildItem proto -Filter *.proto).Name | ForEach-Object { "proto/$_" }
	if (Test-Stale 'sim/core/proto/api.pb.go' @('proto')) {
		Write-Step 'protoc (Go)'
		Invoke-Native 'protoc' (@('-I=proto', '--go_out=sim/core') + $protoFiles)
	}
	if (Test-Stale 'ui/core/proto/api.ts' @('proto')) {
		Write-Step 'protoc (TypeScript)'
		New-Item -ItemType Directory -Force 'ui/core/proto' | Out-Null
		$tsPlugin = "--plugin=protoc-gen-ts=$(Join-Path $Root 'node_modules/.bin/protoc-gen-ts.cmd')"
		Invoke-Native 'protoc' @($tsPlugin, '--ts_opt', 'generate_dependencies', '--ts_out', 'ui/core/proto', '--proto_path', 'proto', 'proto/api.proto')
		Invoke-Native 'protoc' @($tsPlugin, '--ts_out', 'ui/core/proto', '--proto_path', 'proto', 'proto/test.proto')
		Invoke-Native 'protoc' @($tsPlugin, '--ts_out', 'ui/core/proto', '--proto_path', 'proto', 'proto/ui.proto')
	}
}

function Invoke-Wasm {
	Invoke-Proto
	New-Item -ItemType Directory -Force $OutDir | Out-Null
	$wasm = Join-Path $OutDir 'lib.wasm'
	if (Test-Stale $wasm @('sim', 'proto', 'assets/database/db.bin')) {
		Write-Step 'Compiling sim to WebAssembly'
		$env:GOOS = 'js'; $env:GOARCH = 'wasm'
		try { Invoke-Native 'go' @('build', '-o', $wasm, './sim/wasm/') }
		finally { Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue }
	}
}

function New-HtmlIndex([string]$specDir) {
	$spec = Split-Path $specDir -Leaf
	$title = (($spec -split '_') | ForEach-Object { $_.Substring(0, 1).ToUpper() + $_.Substring(1) }) -join ' '
	$html = (Get-Content 'ui/index_template.html' -Raw -Encoding UTF8).
		Replace('@@TITLE@@', "$SiteTitle $title Simulator").
		Replace('@@SPEC@@', $spec)
	[IO.File]::WriteAllText((Join-Path $specDir 'index.html'), $html, (New-Object Text.UTF8Encoding($false)))
}

function Invoke-UI {
	Invoke-Proto

	# ui/core/index.ts imports every core module (the makefile generates this with find/awk).
	Write-Step 'Generating ui/core/index.ts'
	$coreDir = (Resolve-Path 'ui/core').Path
	$imports = Get-ChildItem $coreDir -Recurse -Filter *.ts -File |
		ForEach-Object { $_.FullName.Substring($coreDir.Length + 1).Replace('\', '/') } |
		Where-Object { $_ -ne 'index.ts' } | Sort-Object |
		ForEach-Object { 'import "./' + ($_ -replace '\.ts$', '') + '";' }
	[IO.File]::WriteAllText((Join-Path $coreDir 'index.ts'), (($imports -join "`n") + "`n"))

	Write-Step 'Generating spec index.html files'
	Get-ChildItem ui -Directory | Where-Object { $_.Name -notin @('core', 'scss', 'shared', 'worker') } |
		ForEach-Object { New-HtmlIndex $_.FullName }

	Write-Step 'Writing web workers'
	New-Item -ItemType Directory -Force $OutDir | Out-Null
	$goroot = (& go env GOROOT).Trim()
	$wasmExec = @('lib/wasm/wasm_exec.js', 'misc/wasm/wasm_exec.js') | ForEach-Object { Join-Path $goroot $_ } |
		Where-Object { Test-Path $_ } | Select-Object -First 1
	if (-not $wasmExec) { throw "wasm_exec.js not found under $goroot" }
	$worker = (Get-Content $wasmExec -Raw) + "`n" + (Get-Content 'ui/worker/sim_worker.js' -Raw)
	[IO.File]::WriteAllText((Join-Path $OutDir 'sim_worker.js'), $worker)
	Copy-Item 'ui/worker/net_worker.js' $OutDir -Force

	Write-Step 'Type-checking (tsc)'
	Invoke-Native 'npx' @('tsc', '--noEmit')
	Write-Step 'Bundling (vite)'
	Invoke-Native 'npx' @('vite', 'build')

	Write-Step 'Copying assets'
	$assetsOut = Join-Path $OutDir 'assets'
	New-Item -ItemType Directory -Force $assetsOut | Out-Null
	Get-ChildItem assets | Where-Object { $_.Name -ne 'db_inputs' } |
		ForEach-Object { Copy-Item $_.FullName $assetsOut -Recurse -Force }
}

function Invoke-Dist {
	Invoke-Wasm
	Invoke-UI
	Write-Host "Client built into $OutDir" -ForegroundColor Green
}

function Invoke-BinaryDist {
	Invoke-Dist
	Write-Step 'Preparing binary_dist'
	Remove-Item binary_dist -Recurse -Force -ErrorAction SilentlyContinue
	New-Item -ItemType Directory -Force binary_dist | Out-Null
	Copy-Item $OutDir binary_dist/wotlk -Recurse
	Remove-Item binary_dist/wotlk/lib.wasm -Force
	Remove-Item binary_dist/wotlk/assets/db_inputs -Recurse -Force -ErrorAction SilentlyContinue
	Remove-Item binary_dist/wotlk/assets/database/db.bin, binary_dist/wotlk/assets/database/leftover_db.bin -Force -ErrorAction SilentlyContinue
	Copy-Item sim/web/dist.go.tmpl binary_dist/dist.go
}

function Invoke-EnsureBinaryDistStub {
	# sim/web imports the binary_dist package; tests only need it to compile.
	if (-not (Test-Path binary_dist/dist.go)) {
		New-Item -ItemType Directory -Force binary_dist/wotlk | Out-Null
		New-Item -ItemType File -Force binary_dist/wotlk/embedded | Out-Null
		Copy-Item sim/web/dist.go.tmpl binary_dist/dist.go
	}
}

function Invoke-DevServer([switch]$SkipClient) {
	if (-not $SkipClient) { Invoke-BinaryDist } else { Invoke-Proto; Invoke-EnsureBinaryDistStub }
	Write-Step 'Compiling wowsimwotlk.exe'
	Invoke-Native 'go' @('build', '-o', 'wowsimwotlk.exe', './sim/web/main.go')
	Write-Host 'Build Completed Successfully' -ForegroundColor Green
}

function Invoke-Release {
	Invoke-BinaryDist
	Copy-Item assets/favicon_io/icon-windows_amd64.syso sim/web/icon-windows_amd64.syso
	try {
		Push-Location sim/web
		$env:GOAMD64 = 'v2'
		Invoke-Native 'go' @('build', '-o', (Join-Path $Root 'wowsimwotlk-windows.exe'), '-ldflags=-s -w')
		Pop-Location
		Push-Location cmd/wowsimcli
		Invoke-Native 'go' @('build', '-o', (Join-Path $Root 'wowsimcli-windows.exe'), '--tags=with_db', '-ldflags=-s -w')
		Pop-Location
	} finally {
		Remove-Item Env:GOAMD64 -ErrorAction SilentlyContinue
		Remove-Item sim/web/icon-windows_amd64.syso -ErrorAction SilentlyContinue
		Set-Location $Root
	}
}

function Invoke-Test {
	Invoke-Proto
	Invoke-EnsureBinaryDistStub
	Write-Step 'go test --tags=with_db ./sim/...'
	Invoke-Native 'go' @('test', '--tags=with_db', './sim/...')
}

function Invoke-UpdateTests {
	Get-ChildItem -Recurse -Filter *.results.tmp -File | ForEach-Object {
		$dest = $_.FullName -replace '\.tmp$', ''
		Copy-Item $_.FullName $dest -Force
		Write-Host "updated $($dest.Substring($Root.Length + 1))"
	}
}

function Invoke-Items {
	Invoke-Proto
	Write-Step 'Generating item database'
	Invoke-Native 'go' @('run', './tools/database/gen_db', '-outDir=./assets', '-gen=db')
}

function Invoke-CustomContent {
	Invoke-Proto
	if (-not (Test-Path 'cc_data')) { throw 'cc_data/ not found. See cc_data/README.md for the expected layout.' }
	Write-Step 'Importing custom content from cc_data/'
	Invoke-Native 'go' @('run', './tools/cc/extract')
	Invoke-Items
	Write-Host 'Review assets/db_inputs/cc/REPORT.md and STOCK_CHANGES.md, then run ./build.ps1 test.' -ForegroundColor Green
}

function Invoke-Fmt {
	Invoke-Native 'gofmt' @('-w', './sim', './tools')
}

function Invoke-Clean {
	Remove-Item -Recurse -Force -ErrorAction SilentlyContinue `
		dist, binary_dist, wowsimwotlk.exe, wowsimwotlk-windows.exe, wowsimcli-windows.exe, ui/core/index.ts
	Get-ChildItem ui/core/proto -Filter *.ts -ErrorAction SilentlyContinue | Remove-Item -Force
	Get-ChildItem sim/core/proto -Filter *.pb.go -ErrorAction SilentlyContinue | Remove-Item -Force
	Get-ChildItem ui -Directory | ForEach-Object { Remove-Item (Join-Path $_.FullName 'index.html') -ErrorAction SilentlyContinue }
	Get-ChildItem -Recurse -Filter *.results.tmp -File | Remove-Item -Force
}

switch ($Target) {
	'setup' { Invoke-Setup }
	'proto' { Invoke-Proto }
	'wasm' { Invoke-Wasm }
	'ui' { Invoke-UI }
	'dist' { Invoke-Dist }
	'host' {
		Invoke-Dist
		Write-Host "Serving at http://localhost:$Port/wotlk/  (Ctrl+C to stop)" -ForegroundColor Green
		# Serve one level up so the site lives under /wotlk/ exactly like the upstream GitHub pages.
		Invoke-Native 'npx' @('http-server', 'dist', '-p', "$Port", '-c-1')
	}
	'devserver' { Invoke-DevServer }
	'rundevserver' {
		Invoke-Dist
		Invoke-DevServer -SkipClient
		& ./wowsimwotlk.exe --usefs=true --launch=false --host=":3333"
	}
	'release' { Invoke-Release }
	'test' { Invoke-Test }
	'update-tests' { Invoke-UpdateTests }
	'items' { Invoke-Items }
	'cc' { Invoke-CustomContent }
	'inspect' { Invoke-Proto; Invoke-Native 'go' (@('run', './tools/cc/inspect') + $Rest) }
	'dump' { Invoke-Native 'go' (@('run', './tools/cc/dump') + $Rest) }
	'audit' { Invoke-Proto; Invoke-Native 'go' (@('run', './tools/cc/audit') + $Rest) }
	'fmt' { Invoke-Fmt }
	'clean' { Invoke-Clean }
}
