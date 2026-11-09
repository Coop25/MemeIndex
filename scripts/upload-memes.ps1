<#
.SYNOPSIS
Uploads a folder of files to a MemeIndex server through the normal upload API.

.DESCRIPTION
Sends the files in batches to POST /api/memes, the same endpoint the browser
uses, so duplicate detection, thumbnails and tag suggestions all behave as they
do for a manual upload. Works on Windows PowerShell 5.1 and PowerShell 7+.

Authenticate with either -SessionToken (the raw token) or -CookieValue (the
exact memeindex_session cookie value copied from the browser). Leave both out
only for a server running with MEMEINDEX_ALLOW_ANONYMOUS=true.

.EXAMPLE
.\scripts\upload-memes.ps1 -FolderPath "F:\Memes\Backlog" -BaseUrl "https://memes.example.com" -CookieValue "..." -Recurse
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$FolderPath,
    [Parameter(Mandatory = $true)][string]$BaseUrl,
    [string]$SessionToken = "",
    [string]$CookieValue = "",
    [switch]$Recurse,
    [ValidateRange(1, 100)][int]$BatchSize = 10,
    [string]$Tags = "",
    [string]$Notes = "",
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Net.Http

if ($SessionToken -and $CookieValue) {
    throw "Pass either -SessionToken or -CookieValue, not both."
}
if (-not (Test-Path -LiteralPath $FolderPath -PathType Container)) {
    throw "Folder not found: $FolderPath"
}

$files = @(Get-ChildItem -LiteralPath $FolderPath -File -Recurse:$Recurse | Sort-Object FullName)
if ($files.Count -eq 0) {
    Write-Host "No files found in $FolderPath."
    return
}

if ($DryRun) {
    $files | ForEach-Object { Write-Host $_.FullName }
    Write-Host ("Dry run: {0} file(s) would be uploaded in batches of {1}." -f $files.Count, $BatchSize)
    return
}

# The server stores the session cookie as unpadded base64url of the raw token.
$cookie = $CookieValue.Trim()
if ($SessionToken) {
    $encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($SessionToken.Trim()))
    $cookie = $encoded.TrimEnd("=").Replace("+", "-").Replace("/", "_")
}

$uploadUri = $BaseUrl.TrimEnd("/") + "/api/memes"
$handler = New-Object System.Net.Http.HttpClientHandler
$handler.UseCookies = $false
$client = New-Object System.Net.Http.HttpClient($handler)
$client.Timeout = [TimeSpan]::FromMinutes(30)

$mimeTypes = @{
    ".jpg" = "image/jpeg"; ".jpeg" = "image/jpeg"; ".jfif" = "image/jpeg"; ".png" = "image/png"; ".gif" = "image/gif"
    ".webp" = "image/webp"; ".avif" = "image/avif"; ".bmp" = "image/bmp"; ".svg" = "image/svg+xml"; ".heic" = "image/heic"
    ".mp4" = "video/mp4"; ".m4v" = "video/mp4"; ".webm" = "video/webm"; ".mov" = "video/quicktime"; ".mkv" = "video/x-matroska"; ".avi" = "video/x-msvideo"
    ".mp3" = "audio/mpeg"; ".wav" = "audio/wav"; ".ogg" = "audio/ogg"; ".m4a" = "audio/mp4"; ".flac" = "audio/flac"; ".opus" = "audio/opus"
    ".txt" = "text/plain"; ".pdf" = "application/pdf"; ".zip" = "application/zip"; ".json" = "application/json"
}

$created = 0
$skipped = 0
$failed = New-Object System.Collections.Generic.List[string]

try {
    for ($start = 0; $start -lt $files.Count; $start += $BatchSize) {
        $end = [Math]::Min($start + $BatchSize, $files.Count) - 1
        $batch = $files[$start..$end]
        $content = New-Object System.Net.Http.MultipartFormDataContent
        $streams = @()
        try {
            foreach ($file in $batch) {
                $stream = [IO.File]::OpenRead($file.FullName)
                $streams += $stream
                $part = New-Object System.Net.Http.StreamContent($stream)
                # The server files a meme under the type the upload declares, the
                # way a browser declares it, so name the real type here.
                $mime = $mimeTypes[$file.Extension.ToLowerInvariant()]
                if (-not $mime) { $mime = "application/octet-stream" }
                $part.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::Parse($mime)
                $content.Add($part, "file", $file.Name)
            }
            if ($Tags) { $content.Add((New-Object System.Net.Http.StringContent($Tags)), "tags") }
            if ($Notes) { $content.Add((New-Object System.Net.Http.StringContent($Notes)), "notes") }

            $request = New-Object System.Net.Http.HttpRequestMessage([System.Net.Http.HttpMethod]::Post, $uploadUri)
            $request.Content = $content
            if ($cookie) { [void]$request.Headers.TryAddWithoutValidation("Cookie", "memeindex_session=$cookie") }

            $response = $client.SendAsync($request).GetAwaiter().GetResult()
            $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
            $status = [int]$response.StatusCode

            if ($status -eq 401) {
                throw "The server answered 401: the session token is missing, expired, or was revoked by a logout. Copy a fresh one and run again."
            }
            if ($status -eq 429) {
                throw "The server answered 429 (rate limited). Wait a minute, or lower -BatchSize, and run again; files already uploaded are skipped as duplicates."
            }
            if (-not $response.IsSuccessStatusCode) {
                Write-Warning ("Batch {0}-{1} failed with HTTP {2}: {3}" -f ($start + 1), ($end + 1), $status, $body.Trim())
                $batch | ForEach-Object { $failed.Add($_.FullName) }
                continue
            }

            $result = $body | ConvertFrom-Json
            $created += [int]$result.created
            $skipped += [int]$result.skipped
            # The server saves what it can and lists the files it could not.
            foreach ($entry in @($result.failed)) {
                if ($entry -and $entry.filename) { $failed.Add([string]$entry.filename) }
            }
            Write-Host ("Uploaded {0}/{1} (created {2}, duplicates skipped {3}, failed {4})" -f ($end + 1), $files.Count, $created, $skipped, $failed.Count)
        }
        finally {
            $streams | ForEach-Object { $_.Dispose() }
            $content.Dispose()
        }
    }
}
finally {
    $client.Dispose()
}

Write-Host ("Done: {0} created, {1} duplicates skipped, {2} failed." -f $created, $skipped, $failed.Count)
if ($failed.Count -gt 0) {
    Write-Host "Failed files:"
    $failed | ForEach-Object { Write-Host "  $_" }
    exit 1
}
