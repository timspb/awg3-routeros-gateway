$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$EvidenceDir = Join-Path $PSScriptRoot 'evidence'
New-Item -ItemType Directory -Force -Path $EvidenceDir | Out-Null

$GatewaySourceCommit = (git -C $Root rev-parse HEAD).Trim()
if ([string]::IsNullOrWhiteSpace($GatewaySourceCommit)) {
    throw 'unable to resolve gateway source commit'
}

function Invoke-DockerStep {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,

        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $LogPath = Join-Path $EvidenceDir "$Name.log"
    if (Test-Path $LogPath) {
        Remove-Item -Force $LogPath
    }

    & docker @Arguments 2>&1 | Tee-Object -FilePath $LogPath
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE; see $LogPath"
    }
}

$BakeBase = @('--set', "base.args.GATEWAY_SOURCE_COMMIT=$GatewaySourceCommit")

Invoke-DockerStep -Name 'docker-version' -Arguments @('version')
Invoke-DockerStep -Name 'buildx-version' -Arguments @('buildx', 'version')
Invoke-DockerStep -Name 'buildx-inspect' -Arguments @('buildx', 'inspect', '--bootstrap')
Invoke-DockerStep -Name 'bake-test' -Arguments (@('buildx', 'bake') + $BakeBase + @('test'))
Invoke-DockerStep -Name 'bake-image-amd64' -Arguments (@('buildx', 'bake') + $BakeBase + @('image-amd64'))
Invoke-DockerStep -Name 'bake-image-arm64' -Arguments (@('buildx', 'bake') + $BakeBase + @('image-arm64'))
Invoke-DockerStep -Name 'bake-image-armv5' -Arguments (@('buildx', 'bake') + $BakeBase + @('image-armv5'))
Invoke-DockerStep -Name 'bake-image-multiarch' -Arguments (@('buildx', 'bake') + $BakeBase + @('image-multiarch'))
