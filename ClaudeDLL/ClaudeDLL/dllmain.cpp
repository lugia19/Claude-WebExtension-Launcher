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

static void PatchHash(HMODULE hDll) {
    Log("PatchHash started");

    // Get exe module info
    HMODULE hExe = GetModuleHandle(NULL);
    MODULEINFO modInfo;
    if (!GetModuleInformation(GetCurrentProcess(), hExe, &modInfo, sizeof(modInfo))) {
        Log("Failed to get module info");
        return;
    }

    BYTE* base = (BYTE*)modInfo.lpBaseOfDll;
    DWORD size = modInfo.SizeOfImage;

    // Find the expected hash in process memory
    const char* sentinel = "\"alg\":\"SHA256\",\"value\":\"";
    size_t sentinelLen = strlen(sentinel);
    char* hashLocation = NULL;

    for (DWORD i = 0; i < size - sentinelLen - 64; i++) {
        if (memcmp(base + i, sentinel, sentinelLen) == 0) {
            hashLocation = (char*)(base + i + sentinelLen);
            break;
        }
    }

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
    DWORD oldProtect;
    VirtualProtect(hashLocation, 64, PAGE_READWRITE, &oldProtect);
    memcpy(hashLocation, actualHash, 64);
    VirtualProtect(hashLocation, 64, oldProtect, &oldProtect);
    Log("Patch applied successfully");
}

BOOL APIENTRY DllMain(HMODULE hModule, DWORD ul_reason_for_call, LPVOID lpReserved) {
    if (ul_reason_for_call == DLL_PROCESS_ATTACH) {
        PatchHash(hModule);
    }
    return TRUE;
}