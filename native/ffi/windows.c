/* Native FFI v1. No Mio object addresses enter this module. */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>
#define API __declspec(dllexport)
#define CAP 1024
enum { OK, INVALID_HANDLE, LOAD_ERROR, SYMBOL_ERROR, SIGNATURE_ERROR, BUSY,
       INVALID_INPUT, CAPACITY, MEMORY_ERROR };
typedef struct { uint64_t id; HMODULE module; unsigned active; } Library;
typedef struct { uint64_t id, library, signature; FARPROC address; } Symbol;
typedef struct { uint64_t id; size_t size; unsigned char *data; } Buffer;
static Library libraries[CAP];
static Symbol symbols[CAP];
static Buffer buffers[CAP];
static uint64_t sequence;
static SRWLOCK lock = SRWLOCK_INIT;
static __declspec(thread) uint32_t error_code, os_error;
static void error(uint32_t code, uint32_t os) { error_code = code; os_error = os; }
static uint64_t identity(unsigned slot) {
    /* Never wrap: stale IDs can never identify a later resource. */
    if (sequence >= (UINT64_MAX >> 10)) return 0;
    return (++sequence << 10) | slot;
}
static Library *library(uint64_t id) {
    Library *p = &libraries[id & (CAP - 1)];
    return id && p->id == id ? p : NULL;
}
static Symbol *symbol(uint64_t id) {
    Symbol *p = &symbols[id & (CAP - 1)];
    return id && p->id == id ? p : NULL;
}
static Buffer *buffer(uint64_t id) {
    Buffer *p = &buffers[id & (CAP - 1)];
    return id && p->id == id ? p : NULL;
}
API uint32_t mio_ffi_error(void) { return error_code; }
API uint32_t mio_ffi_os_error(void) { return os_error; }
API uint32_t mio_ffi_resources(void) {
    uint32_t count = 0; unsigned i; AcquireSRWLockExclusive(&lock);
    for (i = 0; i < CAP; ++i) count += !!libraries[i].id + !!symbols[i].id + !!buffers[i].id;
    ReleaseSRWLockExclusive(&lock); return count;
}
API uint64_t mio_ffi_buffer_new(uint64_t size) {
    unsigned i; uint64_t id = 0;
    AcquireSRWLockExclusive(&lock); error(OK, 0);
    for (i = 0; i < CAP; ++i) if (!buffers[i].id) {
        if (size > SIZE_MAX - 1 || !(id = identity(i))) { error(CAPACITY, 0); break; }
        buffers[i].data = calloc((size_t)size + 1, 1);
        if (!buffers[i].data) { error(MEMORY_ERROR, 0); id = 0; break; }
        buffers[i].size = (size_t)size; buffers[i].id = id; break;
    }
    if (i == CAP) error(CAPACITY, 0);
    ReleaseSRWLockExclusive(&lock); return id;
}
API uint32_t mio_ffi_buffer_free(uint64_t id) {
    uint32_t ok = 0; AcquireSRWLockExclusive(&lock);
    Buffer *b = buffer(id); error(INVALID_HANDLE, 0);
    if (b) { free(b->data); memset(b, 0, sizeof(*b)); error(OK, 0); ok = 1; }
    ReleaseSRWLockExclusive(&lock); return ok;
}
API void *mio_ffi_buffer_data(uint64_t id) {
    void *p = NULL; AcquireSRWLockExclusive(&lock);
    Buffer *b = buffer(id); error(INVALID_HANDLE, 0);
    if (b) { p = b->data; error(OK, 0); }
    ReleaseSRWLockExclusive(&lock); return p;
}
API uint32_t mio_ffi_buffer_set(uint64_t id, uint64_t index, uint8_t value) {
    uint32_t ok = 0; AcquireSRWLockExclusive(&lock);
    Buffer *b = buffer(id); error(INVALID_INPUT, 0);
    if (b && index < b->size) { b->data[index] = value; error(OK, 0); ok = 1; }
    ReleaseSRWLockExclusive(&lock); return ok;
}
API uint8_t mio_ffi_buffer_get(uint64_t id, uint64_t index) {
    uint8_t value = 0; AcquireSRWLockExclusive(&lock);
    Buffer *b = buffer(id); error(INVALID_INPUT, 0);
    if (b && index < b->size) { value = b->data[index]; error(OK, 0); }
    ReleaseSRWLockExclusive(&lock); return value;
}
/* Read an out-parameter pointer without permitting integer address casts. */
API void *mio_ffi_buffer_pointer(uint64_t id) {
    void *p = NULL; AcquireSRWLockExclusive(&lock); error(INVALID_INPUT, 0);
    Buffer *b = buffer(id);
    if (b && b->size >= sizeof(p)) { memcpy(&p, b->data, sizeof(p)); error(OK, 0); }
    ReleaseSRWLockExclusive(&lock); return p;
}
/* UTF-8 is copied and strictly converted to UTF-16; embedded NUL is rejected.
   Bare names search System32 only. Other paths resolve against the executable
   directory (never the current working directory), then use an absolute path. */
API uint64_t mio_ffi_open(uint64_t path) {
    wchar_t wide[32768], full[32768]; unsigned i; uint64_t id = 0;
    AcquireSRWLockExclusive(&lock); error(INVALID_INPUT, 0);
    Buffer *b = buffer(path);
    if (!b || !b->size || b->size > 32767 || memchr(b->data, 0, b->size)) goto done;
    int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, (char *)b->data, (int)b->size, wide, 32767);
    if (!n) { error(INVALID_INPUT, GetLastError()); goto done; } wide[n] = 0;
    DWORD flags = LOAD_LIBRARY_SEARCH_SYSTEM32;
    wchar_t *name = wide;
    if (!wcschr(wide, L'/') && !wcschr(wide, L'\\') && !wcschr(wide, L':')) {
        DWORD len = GetSystemDirectoryW(full, 32768);
        if (!len || len + 1 + n >= 32768) { error(LOAD_ERROR, ERROR_INSUFFICIENT_BUFFER); goto done; }
        full[len] = L'\\'; wcscpy_s(full + len + 1, 32768 - len - 1, wide); name = full;
    }
    if (wcschr(wide, L'/') || wcschr(wide, L'\\') || wcschr(wide, L':')) {
        int absolute = (n > 2 && wide[1] == L':' && (wide[2] == L'\\' || wide[2] == L'/')) ||
                       (n > 1 && wide[0] == L'\\' && wide[1] == L'\\');
        if (!absolute) {
            /* Reject drive-relative and root-relative paths, whose meaning
               would depend on process-global current-directory state. */
            if (wcschr(wide, L':') || wide[0] == L'\\' || wide[0] == L'/') goto done;
            DWORD len = GetModuleFileNameW(NULL, full, 32768);
            if (!len || len >= 32768) { error(LOAD_ERROR, GetLastError()); goto done; }
            wchar_t *slash = wcsrchr(full, L'\\');
            if (!slash || (size_t)(slash - full + 1) + n >= 32768) goto done;
            wcscpy_s(slash + 1, 32768 - (size_t)(slash - full + 1), wide); name = full;
        }
        flags |= LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR;
    }
    for (i = 0; i < CAP; ++i) if (!libraries[i].id) break;
    if (i == CAP || !(id = identity(i))) { error(CAPACITY, 0); id = 0; goto done; }
    HMODULE module = LoadLibraryExW(name, NULL, flags);
    if (!module) { error(LOAD_ERROR, GetLastError()); id = 0; goto done; }
    /* LoadLibraryEx can map an EXE without resolving its imports. Such an image
       is not a callable DLL even if it happens to export executable addresses. */
    const IMAGE_DOS_HEADER *dos = (const IMAGE_DOS_HEADER *)module;
    const IMAGE_NT_HEADERS64 *nt = (const IMAGE_NT_HEADERS64 *)((const unsigned char *)module + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE || nt->FileHeader.Machine != IMAGE_FILE_MACHINE_AMD64 ||
        !(nt->FileHeader.Characteristics & IMAGE_FILE_DLL)) {
        FreeLibrary(module); error(LOAD_ERROR, ERROR_BAD_EXE_FORMAT); id = 0; goto done;
    }
    libraries[i].id = id; libraries[i].module = module; error(OK, 0);
done:
    ReleaseSRWLockExclusive(&lock); return id;
}
API uint64_t mio_ffi_symbol(uint64_t owner, uint64_t name, uint64_t signature) {
    uint64_t id = 0; unsigned i; AcquireSRWLockExclusive(&lock); error(INVALID_HANDLE, 0);
    Library *lib = library(owner); Buffer *b = buffer(name);
    if (!lib || !b) goto done;
    if (!b->size || b->size > 255 || memchr(b->data, 0, b->size)) { error(INVALID_INPUT, 0); goto done; }
    for (i = 0; i < b->size; ++i) if (b->data[i] < 33 || b->data[i] > 126) { error(INVALID_INPUT, 0); goto done; }
    FARPROC address = GetProcAddress(lib->module, (char *)b->data);
    if (!address) { error(SYMBOL_ERROR, GetLastError()); goto done; }
    MEMORY_BASIC_INFORMATION region;
    if (!VirtualQuery((const void *)address, &region, sizeof(region)) ||
        region.State != MEM_COMMIT || (region.Protect & (PAGE_GUARD | PAGE_NOACCESS)) ||
        !(region.Protect & (PAGE_EXECUTE | PAGE_EXECUTE_READ | PAGE_EXECUTE_READWRITE | PAGE_EXECUTE_WRITECOPY))) {
        error(SYMBOL_ERROR, ERROR_INVALID_ADDRESS); goto done;
    }
    for (i = 0; i < CAP; ++i) if (!symbols[i].id) break;
    if (i == CAP || !(id = identity(i))) { error(CAPACITY, 0); id = 0; goto done; }
    symbols[i].id = id; symbols[i].library = owner; symbols[i].signature = signature;
    symbols[i].address = address; error(OK, 0);
done:
    ReleaseSRWLockExclusive(&lock); return id;
}
API uint32_t mio_ffi_release(uint64_t id) {
    uint32_t ok = 0; AcquireSRWLockExclusive(&lock); error(INVALID_HANDLE, 0);
    Symbol *s = symbol(id);
    if (s) {
        Library *lib = library(s->library);
        if (lib && lib->active) error(BUSY, 0);
        else { memset(s, 0, sizeof(*s)); error(OK, 0); ok = 1; }
    }
    ReleaseSRWLockExclusive(&lock); return ok;
}
API uint32_t mio_ffi_close(uint64_t id) {
    unsigned i; uint32_t ok = 0; AcquireSRWLockExclusive(&lock); error(INVALID_HANDLE, 0);
    Library *lib = library(id);
    if (!lib) goto done;
    if (lib->active) { error(BUSY, 0); goto done; }
    /* Invalidate every symbol atomically with unload. No dangling function token. */
    if (!FreeLibrary(lib->module)) { error(LOAD_ERROR, GetLastError()); goto done; }
    for (i = 0; i < CAP; ++i) if (symbols[i].library == id) memset(&symbols[i], 0, sizeof(symbols[i]));
    memset(lib, 0, sizeof(*lib)); error(OK, 0); ok = 1;
done:
    ReleaseSRWLockExclusive(&lock); return ok;
}
/* Used only by the generated bridge. Acquire pins the DLL throughout the call.
   Invalid/stale/mistyped calls terminate with a deterministic diagnostic exit;
   callers can use check() for recoverable validation before invocation. */
API uint32_t mio_ffi_check(uint64_t id, uint64_t signature) {
    uint32_t ok = 0; AcquireSRWLockExclusive(&lock); error(INVALID_HANDLE, 0);
    Symbol *s = symbol(id);
    if (s && library(s->library)) {
        error(SIGNATURE_ERROR, 0);
        if (s->signature == signature) { ok = 1; error(OK, 0); }
    }
    ReleaseSRWLockExclusive(&lock); return ok;
}
API FARPROC mio_ffi_address(uint64_t id) {
    FARPROC address = NULL; AcquireSRWLockExclusive(&lock); error(INVALID_HANDLE, 0);
    Symbol *s = symbol(id);
    if (s && library(s->library)) { address = s->address; error(OK, 0); }
    ReleaseSRWLockExclusive(&lock); return address;
}
API FARPROC mio_ffi_acquire(uint64_t id, uint64_t signature) {
    FARPROC address = NULL; AcquireSRWLockExclusive(&lock);
    Symbol *s = symbol(id); Library *lib = s ? library(s->library) : NULL;
    if (lib && lib->active < UINT32_MAX && s->signature == signature) { ++lib->active; address = s->address; }
    ReleaseSRWLockExclusive(&lock);
    if (!address) {
        static const char message[] = "E_FFI_CALL invalid, closed or mismatched native function token\n";
        DWORD written; WriteFile(GetStdHandle(STD_ERROR_HANDLE), message, sizeof(message)-1, &written, NULL);
        ExitProcess(125);
    }
    return address;
}
API void mio_ffi_finish(uint64_t id) {
    AcquireSRWLockExclusive(&lock);
    Symbol *s = symbol(id); Library *lib = s ? library(s->library) : NULL;
    if (lib && lib->active) --lib->active;
    ReleaseSRWLockExclusive(&lock);
}
API FARPROC mio_ffi_dispatch(uint64_t id, uint64_t signature, uint64_t finish) {
    if (!finish) return mio_ffi_acquire(id, signature);
    mio_ffi_finish(id); return NULL;
}
