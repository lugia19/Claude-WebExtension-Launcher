#define _CRT_SECURE_NO_WARNINGS

#include <windows.h>
#include <string.h>
#include <stdio.h>
#include <psapi.h>

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

#define MAX_HASH_LEN 256

static void TrimNewline(char* str) {
    size_t len = strlen(str);
    while (len > 0 && (str[len - 1] == '\n' || str[len - 1] == '\r')) {
        str[--len] = '\0';
    }
}

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

static void PatchHash(HMODULE hDll) {
    // Build path to hashes file next to the DLL
    char hashPath[MAX_PATH];
    GetModuleFileNameA(hDll, hashPath, MAX_PATH);
    char* lastSlash = strrchr(hashPath, '\\');
    if (!lastSlash) return;
    strcpy(lastSlash + 1, "hashes");

    // Try to open the file
    FILE* f = fopen(hashPath, "r");
    Log("PatchHash started");
    if (!f) return;

    char oldHash[MAX_HASH_LEN] = { 0 };
    char newHash[MAX_HASH_LEN] = { 0 };

    if (!fgets(oldHash, MAX_HASH_LEN, f) || !fgets(newHash, MAX_HASH_LEN, f)) {
        fclose(f);
        return;
    }
    fclose(f);

    TrimNewline(oldHash);
    TrimNewline(newHash);

    Log(oldHash);
    Log(newHash);

    if (strlen(oldHash) == 0 || strlen(newHash) == 0) return;
    if (strlen(oldHash) != strlen(newHash)) return;

    // Get exe module info
    HMODULE hExe = GetModuleHandle(NULL);
    MODULEINFO modInfo;
    if (!GetModuleInformation(GetCurrentProcess(), hExe, &modInfo, sizeof(modInfo))) return;

    BYTE* base = (BYTE*)modInfo.lpBaseOfDll;
    DWORD size = modInfo.SizeOfImage;
    size_t hashLen = strlen(oldHash);

    char buf[128];
    sprintf(buf, "Scanning %u bytes at %p", size, base);
    Log(buf);

    // Scan and replace
    for (DWORD i = 0; i < size - hashLen; i++) {
        if (memcmp(base + i, oldHash, hashLen) == 0) {
            Log("Hash found in memory, patching...");
            DWORD oldProtect;
            VirtualProtect(base + i, hashLen, PAGE_READWRITE, &oldProtect);
            memcpy(base + i, newHash, hashLen);
            Log("Patch applied");
            VirtualProtect(base + i, hashLen, oldProtect, &oldProtect);
            break;
        }
    }

}

BOOL APIENTRY DllMain(HMODULE hModule, DWORD ul_reason_for_call, LPVOID lpReserved) {
    if (ul_reason_for_call == DLL_PROCESS_ATTACH) {
        PatchHash(hModule);
    }
    return TRUE;
}