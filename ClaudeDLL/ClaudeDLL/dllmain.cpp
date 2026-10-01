#define _CRT_SECURE_NO_WARNINGS

#include <windows.h>
#include <string.h>
#include <stdio.h>
#include <psapi.h>
#include <bcrypt.h>

#pragma comment(lib, "bcrypt.lib")

#define ORIG_DLL "C:\\Windows\\System32\\version"

#pragma comment(linker, "/export:GetFileVersionInfoA=" ORIG_DLL ".GetFileVersionInfoA,@1")
#pragma comment(linker, "/export:GetFileVersionInfoByHandle=" ORIG_DLL ".GetFileVersionInfoByHandle,@2")
#pragma comment(linker, "/export:GetFileVersionInfoExA=" ORIG_DLL ".GetFileVersionInfoExA,@3")
#pragma comment(linker, "/export:GetFileVersionInfoExW=" ORIG_DLL ".GetFileVersionInfoExW,@4")
#pragma comment(linker, "/export:GetFileVersionInfoSizeA=" ORIG_DLL ".GetFileVersionInfoSizeA,@5")
#pragma comment(linker, "/export:GetFileVersionInfoSizeExA=" ORIG_DLL ".GetFileVersionInfoSizeExA,@6")
#pragma comment(linker, "/export:GetFileVersionInfoSizeExW=" ORIG_DLL ".GetFileVersionInfoSizeExW,@7")
#pragma comment(linker, "/export:GetFileVersionInfoSizeW=" ORIG_DLL ".GetFileVersionInfoSizeW,@8")
#pragma comment(linker, "/export:GetFileVersionInfoW=" ORIG_DLL ".GetFileVersionInfoW,@9")
#pragma comment(linker, "/export:VerFindFileA=" ORIG_DLL ".VerFindFileA,@10")
#pragma comment(linker, "/export:VerFindFileW=" ORIG_DLL ".VerFindFileW,@11")
#pragma comment(linker, "/export:VerInstallFileA=" ORIG_DLL ".VerInstallFileA,@12")
#pragma comment(linker, "/export:VerInstallFileW=" ORIG_DLL ".VerInstallFileW,@13")
#pragma comment(linker, "/export:VerLanguageNameA=" ORIG_DLL ".VerLanguageNameA,@14")
#pragma comment(linker, "/export:VerLanguageNameW=" ORIG_DLL ".VerLanguageNameW,@15")
#pragma comment(linker, "/export:VerQueryValueA=" ORIG_DLL ".VerQueryValueA,@16")
#pragma comment(linker, "/export:VerQueryValueW=" ORIG_DLL ".VerQueryValueW,@17")

#define SHA256_LEN 32
#define HEX_HASH_LEN 65

static void Log(const char* msg) {
    char logPath[MAX_PATH];
    GetModuleFileNameA(NULL, logPath, MAX_PATH);
    char* lastSlash = strrchr(logPath, '\\');
    if (!lastSlash) return;
    strcpy(lastSlash + 1, "patch.log");

    FILE* f = fopen(logPath, "a");
    if (!f) return;
    fprintf(f, "%s\n", msg);
    fclose(f);
}

// Compute SHA256 of the asar header (skip first 16 bytes, hash the JSON string)
static BOOL ComputeAsarHeaderHash(const char* asarPath, char* hexHash) {
    FILE* f = fopen(asarPath, "rb");
    if (!f) return FALSE;

    // First 16 bytes: two Pickle structures
    // Bytes 0-3:   first pickle payload size (always 4)
    // Bytes 4-7:   header size value
    // Bytes 8-11:  second pickle payload size
    // Bytes 12-15: header string length
    unsigned char prefix[16];
    if (fread(prefix, 1, 16, f) != 16) {
        fclose(f);
        return FALSE;
    }

    DWORD stringLen = *(DWORD*)(prefix + 12);

    char buf[128];
    sprintf(buf, "Asar header string length: %u", stringLen);
    Log(buf);

    // Read the header JSON string
    unsigned char* headerData = (unsigned char*)malloc(stringLen);
    if (!headerData) {
        fclose(f);
        return FALSE;
    }

    if (fread(headerData, 1, stringLen, f) != stringLen) {
        free(headerData);
        fclose(f);
        return FALSE;
    }
    fclose(f);

    // SHA256 hash using Windows CNG
    BCRYPT_ALG_HANDLE hAlg = NULL;
    BCRYPT_HASH_HANDLE hHash = NULL;
    BYTE hash[SHA256_LEN];
    BOOL success = FALSE;

    if (BCryptOpenAlgorithmProvider(&hAlg, BCRYPT_SHA256_ALGORITHM, NULL, 0) == 0) {
        if (BCryptCreateHash(hAlg, &hHash, NULL, 0, NULL, 0, 0) == 0) {
            if (BCryptHashData(hHash, headerData, stringLen, 0) == 0) {
                if (BCryptFinishHash(hHash, hash, SHA256_LEN, 0) == 0) {
                    for (int i = 0; i < SHA256_LEN; i++) {
                        sprintf(hexHash + (i * 2), "%02x", hash[i]);
                    }
                    hexHash[64] = '\0';
                    success = TRUE;
                }
            }
            BCryptDestroyHash(hHash);
        }
        BCryptCloseAlgorithmProvider(hAlg, 0);
    }

    free(headerData);
    return success;
}

// FindInExe returns the address just past sentinel in this process's copy of the exe,
// where at least trailing more bytes follow it, or NULL.
static BYTE* FindInExe(const char* sentinel, size_t trailing) {
    MODULEINFO modInfo;
    if (!GetModuleInformation(GetCurrentProcess(), GetModuleHandle(NULL), &modInfo, sizeof(modInfo))) {
        Log("Failed to get module info");
        return NULL;
    }
    BYTE* base = (BYTE*)modInfo.lpBaseOfDll;
    size_t size = modInfo.SizeOfImage, sentinelLen = strlen(sentinel);
    for (size_t i = 0; i + sentinelLen + trailing <= size; i++) {
        if (memcmp(base + i, sentinel, sentinelLen) == 0) {
            return base + i + sentinelLen;
        }
    }
    return NULL;
}

// WriteExe copies n bytes from src over at, in this process's copy of the exe.
static BOOL WriteExe(void* at, const void* src, size_t n) {
    // Execute too, in case the bytes share a page with code.
    DWORD oldProtect;
    if (!VirtualProtect(at, n, PAGE_EXECUTE_READWRITE, &oldProtect)) {
        Log("VirtualProtect failed");
        return FALSE;
    }
    memcpy(at, src, n);
    VirtualProtect(at, n, oldProtect, &oldProtect);
    return TRUE;
}

static void PatchHash(HMODULE hDll) {
    Log("PatchHash started");

    // Find the expected hash in process memory
    char* hashLocation = (char*)FindInExe("\"alg\":\"SHA256\",\"value\":\"", 64);
    if (!hashLocation) {
        Log("Could not find expected hash in exe memory");
        return;
    }

    // Extract the expected hash (64 hex chars)
    char expectedHash[HEX_HASH_LEN];
    memcpy(expectedHash, hashLocation, 64);
    expectedHash[64] = '\0';

    char buf[256];
    sprintf(buf, "Expected hash from exe: %s", expectedHash);
    Log(buf);

    // Build path to app.asar relative to the exe
    char asarPath[MAX_PATH];
    GetModuleFileNameA(NULL, asarPath, MAX_PATH);
    char* lastSlash = strrchr(asarPath, '\\');
    if (!lastSlash) {
        Log("Could not determine exe directory");
        return;
    }
    strcpy(lastSlash + 1, "resources\\app.asar");

    sprintf(buf, "Asar path: %s", asarPath);
    Log(buf);

    // Compute actual hash of the asar header
    char actualHash[HEX_HASH_LEN];
    if (!ComputeAsarHeaderHash(asarPath, actualHash)) {
        Log("Failed to compute asar header hash");
        return;
    }

    sprintf(buf, "Computed hash from asar: %s", actualHash);
    Log(buf);

    // If they already match, nothing to do
    if (memcmp(expectedHash, actualHash, 64) == 0) {
        Log("Hashes already match, no patching needed");
        return;
    }

    // Patch the expected hash in memory with the actual hash
    Log("Hashes differ, patching...");
    if (WriteExe(hashLocation, actualHash, 64)) {
        Log("Patch applied successfully");
    }
}

// EnableNodeCliInspectArguments's index in Electron's fuse wire (see @electron/fuses).
#define FUSE_NODE_CLI_INSPECT 3

// EnableInspectFuse turns on the fuse that lets --inspect start Node's inspector in the
// main process, in this process's copy of the exe, for the launcher's advanced debug
// mode (the --webext-dev-mode marker; see wrapper.js). The file isn't changed. Other
// processes, which don't have the marker, keep the fuse as shipped. (NODE_OPTIONS stays
// off: Electron ignores most of it in a packaged app anyway.)
static void EnableInspectFuse() {
    if (!wcsstr(GetCommandLineW(), L" --webext-dev-mode")) {
        return;
    }
    Log("Advanced debug mode: enabling the --inspect fuse");

    // The wire: the sentinel, a version byte, a length byte, then one byte per fuse:
    // '0' disabled, '1' enabled, 'r' removed.
    BYTE* wire = FindInExe("dL7pKGdnNz796PbbjQWNKmHXBZaB9tsX", 2 + FUSE_NODE_CLI_INSPECT + 1);
    if (!wire) {
        Log("Could not find the fuse wire in exe memory");
        return;
    }

    BYTE version = wire[0], length = wire[1];
    BYTE* fuses = wire + 2;
    char buf[128];
    sprintf(buf, "Fuse wire version %u, %u fuses: %.*s", version, length, (int)length, (const char*)fuses);
    Log(buf);
    if (version != 1 || length <= FUSE_NODE_CLI_INSPECT) {
        Log("Unknown fuse wire layout, leaving it alone");
        return;
    }

    if (fuses[FUSE_NODE_CLI_INSPECT] != '0') {
        Log("The --inspect fuse isn't off, leaving it alone");
        return;
    }
    if (!WriteExe(fuses + FUSE_NODE_CLI_INSPECT, "1", 1)) {
        return;
    }
    sprintf(buf, "Fuses now: %.*s", (int)length, (const char*)fuses);
    Log(buf);
}

BOOL APIENTRY DllMain(HMODULE hModule, DWORD ul_reason_for_call, LPVOID lpReserved) {
    if (ul_reason_for_call == DLL_PROCESS_ATTACH) {
        PatchHash(hModule);
        EnableInspectFuse();
    }
    return TRUE;
}