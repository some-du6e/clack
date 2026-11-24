# cold-disk.ps1 purge | flood FILE STOP SECONDS | probe FILE: a Windows
# runner's system disk (C:) as windows-idle found it when Media
# Foundation's first engine stalled (flake-check mf-cold). C: is a remote
# Premium SSD whose writes cap at 125 to 170 MB/s; behind writes at the cap
# each cold read of it took 50 ms to 2.4 s (flake-check mf-cold-diag,
# 2026-11-23), where D:, the local disk, read in 0 to 1 ms.
#
# purge  writes the modified pages out and drops the standby list: the
#        pages no process has mapped (Media Foundation's DLLs, a test
#        binary not running) are read from the disk again. The DLLs in use
#        stay, as on a machine whose disk just went cold.
# flood  writes FILE (4 GB of random bytes) over and over until STOP
#        exists or SECONDS have gone by, each write through to the disk:
#        written into the cache, a file written again dropped its pages
#        before they reached the disk, and one round of mf-cold-start-red1
#        wrote 28 GB in 12 s and never held C: at its cap. Two of these
#        keep two writes queued at the disk.
# probe  reads 32 4 KB blocks at random from FILE (1 GB on C:, made the
#        first time), after a purge each from the disk, and prints how
#        long they took: what a cold read of C: costs then.
param(
    [Parameter(Mandatory)][ValidateSet('purge', 'flood', 'probe')][string]$Mode,
    [string]$File,
    [string]$Stop,
    [int]$Seconds = 90
)
$ErrorActionPreference = 'Stop'

if ($Mode -eq 'purge') {
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class ColdDisk {
    [DllImport("ntdll.dll")] static extern int NtSetSystemInformation(int cls, ref int info, int len);
    [DllImport("advapi32.dll", SetLastError = true)] static extern bool OpenProcessToken(IntPtr h, uint access, out IntPtr token);
    [DllImport("advapi32.dll", SetLastError = true, CharSet = CharSet.Unicode)] static extern bool LookupPrivilegeValue(string sys, string name, out long luid);
    [StructLayout(LayoutKind.Sequential, Pack = 4)] struct TokPriv { public int Count; public long Luid; public int Attr; }
    [DllImport("advapi32.dll", SetLastError = true)] static extern bool AdjustTokenPrivileges(IntPtr token, bool disableAll, ref TokPriv state, int len, IntPtr prev, IntPtr retLen);
    [DllImport("kernel32.dll")] static extern IntPtr GetCurrentProcess();
    static void Enable(string name) {
        IntPtr tok;
        if (!OpenProcessToken(GetCurrentProcess(), 0x28, out tok)) throw new Exception("OpenProcessToken: " + Marshal.GetLastWin32Error());
        long luid;
        if (!LookupPrivilegeValue(null, name, out luid)) throw new Exception("LookupPrivilegeValue " + name + ": " + Marshal.GetLastWin32Error());
        TokPriv tp = new TokPriv { Count = 1, Luid = luid, Attr = 2 }; // SE_PRIVILEGE_ENABLED
        AdjustTokenPrivileges(tok, false, ref tp, 0, IntPtr.Zero, IntPtr.Zero);
        int err = Marshal.GetLastWin32Error();
        if (err != 0) throw new Exception("AdjustTokenPrivileges " + name + ": " + err);
    }
    // SystemMemoryListInformation (80): MemoryFlushModifiedList 3, then
    // MemoryPurgeStandbyList 4
    public static void Purge() {
        Enable("SeProfileSingleProcessPrivilege");
        foreach (int cmd in new int[] { 3, 4 }) {
            int c = cmd;
            int st = NtSetSystemInformation(80, ref c, 4);
            if (st != 0) throw new Exception(string.Format("NtSetSystemInformation(SystemMemoryListInformation, {0}): 0x{1:x8}", cmd, st));
        }
    }
}
'@
    [ColdDisk]::Purge()
    Write-Host 'cold-disk: the standby list purged'
    exit 0
}

if ($Mode -eq 'probe') {
    if (-not $File) { throw 'cold-disk probe FILE' }
    $size = 1GB
    if (-not (Test-Path -LiteralPath $File)) {
        $buf = New-Object byte[] (4MB)
        (New-Object Random 2).NextBytes($buf)
        $fs = New-Object IO.FileStream($File, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::Read, 4096, [IO.FileOptions]::WriteThrough)
        try { for ($i = 0; $i -lt $size / 4MB; $i++) { $fs.Write($buf, 0, $buf.Length) } } finally { $fs.Dispose() }
        Write-Host "cold-disk: the probe file made ($File)"
        exit 0
    }
    $rng = New-Object Random
    $blk = New-Object byte[] 4096
    $ms = @()
    $fs = New-Object IO.FileStream($File, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite, 1, [IO.FileOptions]::RandomAccess)
    try {
        for ($i = 0; $i -lt 32; $i++) {
            $fs.Position = [long]$rng.Next(0, [int]($size / 4096)) * 4096
            $sw = [Diagnostics.Stopwatch]::StartNew()
            [void]$fs.Read($blk, 0, $blk.Length)
            $ms += $sw.Elapsed.TotalMilliseconds
        }
    } finally { $fs.Dispose() }
    $sorted = $ms | Sort-Object
    Write-Host ('cold-disk: 32 cold reads of C: took {0:N0} ms at the median, {1:N0} ms at most' -f $sorted[16], $sorted[31])
    exit 0
}

if (-not $File -or -not $Stop) { throw 'cold-disk flood FILE STOP SECONDS' }
$buf = New-Object byte[] (4MB)
(New-Object Random 1).NextBytes($buf)
$n = 0
$t0 = Get-Date
$end = $t0.AddSeconds($Seconds)
$going = { -not (Test-Path -LiteralPath $Stop) -and (Get-Date) -lt $end }
while (& $going) {
    $fs = New-Object IO.FileStream($File, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None, 4096, [IO.FileOptions]::WriteThrough)
    try {
        for ($i = 0; $i -lt 1024 -and (& $going); $i++) { $fs.Write($buf, 0, $buf.Length); $n++ }
    } finally { $fs.Dispose() }
}
Remove-Item -LiteralPath $File -Force -ErrorAction SilentlyContinue
$s = ((Get-Date) - $t0).TotalSeconds
Write-Host ('cold-disk: {0:N0} MB written through to the disk in {1:N0} s ({2:N0} MB/s)' -f ($n * 4), $s, ($n * 4 / [Math]::Max($s, 1)))
