# Good TURN — route helper for Windows
# Adds static routes to TURN server IPs via the default gateway.
# Safe to re-run — checks for existing routes.

$gateway = Get-NetRoute `
    -DestinationPrefix "0.0.0.0/0" `
    | Sort-Object RouteMetric `
    | Select-Object -First 1 -ExpandProperty NextHop

if (-not $gateway) {
    Write-Error "Cannot detect default gateway"
    exit 1
}

Write-Host "Default gateway: $gateway"

$input | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq "") { return }

    # Extract IPv4 from line (plain IP or relayed-address=IP:port)
    if ($line -match 'relayed-address=(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):\d+') {
        $addr = $Matches[1]
    } elseif ($line -match '^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})$') {
        $addr = $Matches[1]
    } else {
        return  # skip non-IPv4 lines
    }

    $dest = "$addr/32"
    $existing = Get-NetRoute -DestinationPrefix $dest -ErrorAction SilentlyContinue

    if ($existing | Where-Object { $_.NextHop -eq $gateway }) {
        Write-Host "Route exists: $addr via $gateway"
        return
    }

    if ($existing.Count -gt 0) {
        Write-Host "Updating route: $addr via $gateway"
        $existing | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
    }

    Write-Host "Adding route: $addr via $gateway"
    New-NetRoute `
        -DestinationPrefix $dest `
        -NextHop $gateway `
        -PolicyStore ActiveStore `
        -ErrorAction Stop
}
