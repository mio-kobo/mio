"""DEVELOPMENT AID ONLY (needs `unicorn`): runs a lowerwin PE in an x86-64 emulator with a Python model of the few
KERNEL32 calls the image imports. It is NOT Windows: results from it must never be reported as Windows execution.
Its value is fast local iteration and Win64 ABI assertions at every API call (alignment, shadow space reachable,
volatile registers poisoned afterwards so code relying on them breaks)."""
import struct
import sys

from unicorn import Uc, UC_ARCH_X86, UC_MODE_64, UC_HOOK_CODE, UC_PROT_ALL, UcError
from unicorn.x86_const import *

BASE = 0x140000000
STACK_TOP = 0x7FFF00100000
STACK_SIZE = 8 * 1024 * 1024
HOOKS = 0x7FFE00000000
HEAP = 0x200000000


class Exit(Exception):
    pass


class Emu:
    def __init__(self, data, files=None, stack_size=1024 * 1024, max_instructions=2_000_000_000, stdin=b"", command_line="program.exe", environment=None):
        self.out = bytearray()
        self.err = bytearray()
        self.exit_code = None
        self.violations = []
        self.files = files if files is not None else {}
        self.handles = {-10 & 0xFFFFFFFF: 0, -11 & 0xFFFFFFFF: 1, -12 & 0xFFFFFFFF: 2}
        self.max_instructions = max_instructions
        self.heap_next = HEAP
        self.next_region = HEAP
        self.open_files = {}
        self.dirs = set()
        self.finds = {}
        self.stdin = bytearray(stdin)
        self.command_line = command_line
        self.environment = dict(environment or {})
        self.stdin_pos = 0
        self.last_error = 0
        self.processes = {}
        self.reserved2 = b""
        self.child_instructions = max_instructions
        self.ticks = 123456
        self.reserved = []
        self.committed = set()  # (start, end) intervals; may overlap, only used to avoid double mapping
        self.uc = uc = Uc(UC_ARCH_X86, UC_MODE_64)
        pe = struct.unpack_from("<I", data, 60)[0]
        nsec = struct.unpack_from("<H", data, pe + 6)[0]
        opt = pe + 24
        size_image = struct.unpack_from("<I", data, opt + 56)[0]
        entry = struct.unpack_from("<I", data, opt + 16)[0]
        uc.mem_map(BASE, (size_image + 0xFFF) & ~0xFFF, UC_PROT_ALL)
        uc.mem_write(BASE, data[:struct.unpack_from("<I", data, opt + 60)[0]])
        for i in range(nsec):
            o = opt + 240 + 40 * i
            vsize, rva, rawsize, rawptr = struct.unpack_from("<IIII", data, o + 8)
            uc.mem_write(BASE + rva, data[rawptr:rawptr + min(rawsize, vsize if vsize else rawsize)])
        # imports: fill IAT with hook stubs
        ir = struct.unpack_from("<I", data, opt + 112 + 8)[0]
        uc.mem_map(HOOKS, 0x10000, UC_PROT_ALL)
        self.hook_names = {}
        k = 0
        d = ir
        while True:
            ilt, _, _, name, iat = struct.unpack("<IIIII", bytes(uc.mem_read(BASE + d, 20)))
            if name == 0:
                break
            j = 0
            while True:
                hint = struct.unpack("<Q", bytes(uc.mem_read(BASE + ilt + 8 * j, 8)))[0]
                if hint == 0:
                    break
                sym = bytes(uc.mem_read(BASE + hint + 2, 40)).split(b"\0")[0].decode()
                addr = HOOKS + 16 * k
                uc.mem_write(addr, b"\xc3")
                uc.mem_write(BASE + iat + 8 * j, struct.pack("<Q", addr))
                self.hook_names[addr] = sym
                k += 1
                j += 1
            d += 20
        uc.mem_map(STACK_TOP - STACK_SIZE, STACK_SIZE, UC_PROT_ALL)
        sp = STACK_TOP - 0x1000
        sp -= 8  # as after a call into the entry point: rsp = 8 (mod 16)
        uc.mem_write(sp, struct.pack("<Q", HOOKS + 0xF000))
        uc.mem_write(HOOKS + 0xF000, b"\xc3")
        self.stack_low = STACK_TOP - stack_size
        uc.reg_write(UC_X86_REG_RSP, sp)
        self.entry = BASE + entry
        uc.hook_add(UC_HOOK_CODE, self._code, begin=HOOKS, end=HOOKS + 0x10000)

    def reg(self, r):
        return self.uc.reg_read(r)

    def _violation(self, message):
        self.violations.append(message)

    def _code(self, uc, address, size, _):
        if address == HOOKS + 0xF000:
            self._violation("entry point returned")
            self.exit_code = -1
            uc.emu_stop()
            return
        name = self.hook_names.get(address)
        if name is None:
            return
        rsp = self.reg(UC_X86_REG_RSP)
        if rsp % 16 != 8:
            self._violation("%s called with misaligned stack (rsp=%#x)" % (name, rsp))
        if rsp < self.stack_low + 4096:
            self._violation("stack overflow")
        args = [self.reg(r) for r in (UC_X86_REG_RCX, UC_X86_REG_RDX, UC_X86_REG_R8, UC_X86_REG_R9)]
        stack_arg = lambda n: struct.unpack("<Q", bytes(uc.mem_read(rsp + 8 + 8 * n, 8)))[0]  # [rsp+8+8n]: n=4 is arg5
        result = getattr(self, "api_" + name)(args, stack_arg)
        if self.exit_code is not None:
            uc.emu_stop()
            return
        uc.reg_write(UC_X86_REG_RAX, result & 0xFFFFFFFFFFFFFFFF)
        for r in (UC_X86_REG_RCX, UC_X86_REG_RDX, UC_X86_REG_R8, UC_X86_REG_R9, UC_X86_REG_R10, UC_X86_REG_R11):
            uc.reg_write(r, 0xDEAD0000DEAD0000)  # volatile registers are clobbered by any API call

    def api_GetStdHandle(self, a, s):
        return self.handles.get(a[0] & 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF)

    def api_WriteFile(self, a, s):
        handle, buf, n, written = a
        if s(4) != 0:
            self._violation("lpOverlapped must be NULL")
        data = bytes(self.uc.mem_read(buf, n))
        if handle == 1:
            self.out += data
        elif handle == 2:
            self.err += data
        elif handle in self.open_files:
            f = self.open_files[handle]
            buf_ = self.files[f["name"]]
            if f["append"]:
                buf_ += data
            else:
                buf_[f["pos"]:f["pos"] + n] = data
                f["pos"] += n
        else:
            self.last_error = 6
            return 0
        self.uc.mem_write(written, struct.pack("<I", n))
        return 1

    def api_VirtualAlloc(self, a, s):
        addr, size, kind, protect = a
        if protect != 4:
            self._violation("VirtualAlloc protection %#x" % protect)
        size = (size + 0xFFF) & ~0xFFF
        if kind & 0x2000:  # MEM_RESERVE: choose an address; pages are mapped when committed
            base = self.next_region
            self.next_region += (size + 0xFFFF) & ~0xFFFF
            self.reserved.append((base, size))
            addr = base
            if not kind & 0x1000:
                return base
        if kind & 0x1000:
            if not any(b <= addr and addr + size <= b + sz for b, sz in self.reserved):
                self._violation("commit outside reservation %#x+%#x" % (addr, size))
                return 0
            # map only the not-yet-committed gaps, each in one call
            cursor, end = addr, addr + size
            for lo, hi in sorted(self.committed):
                if hi <= cursor or lo >= end:
                    continue
                if lo > cursor:
                    self.uc.mem_map(cursor, lo - cursor, UC_PROT_ALL)
                cursor = max(cursor, hi)
            if cursor < end:
                self.uc.mem_map(cursor, end - cursor, UC_PROT_ALL)
            self.committed.add((addr, end))
            return addr
        self._violation("VirtualAlloc type %#x" % kind)
        return 0

    # ---- in-memory file system model (paths are compared as UTF-16 decoded text) ----
    def api_MultiByteToWideChar(self, a, s):
        codepage, flags, src, n = a[0] & 0xFFFFFFFF, a[1] & 0xFFFFFFFF, a[2], a[3] & 0xFFFFFFFF
        dst, cch = s(4), s(5) & 0xFFFFFFFF
        if codepage != 65001 or n != 0xFFFFFFFF:
            self._violation("MultiByteToWideChar expects CP_UTF8 and NUL-terminated input")
        raw = b""
        while True:
            c = bytes(self.uc.mem_read(src + len(raw), 1))
            if c == b"\0":
                break
            raw += c
        text = raw.decode("utf-8", "strict") + "\0"
        wide = text.encode("utf-16-le")
        if len(wide) // 2 > cch:
            self.last_error = 122
            return 0
        self.uc.mem_write(dst, wide)
        return len(wide) // 2

    def api_CreateFileW(self, a, s):
        name_ptr, access, share, sec = a
        disposition, flags, template = s(4), s(5), s(6)
        if flags != 0x80 or template != 0:
            self._violation("unexpected CreateFileW flags/template")
        raw = b""
        while True:
            c = bytes(self.uc.mem_read(name_ptr + len(raw), 2))
            if c == b"\0\0":
                break
            raw += c
        name = self._canonical(raw.decode("utf-16-le"))
        if name.upper() == "NUL":  # the null device: always empty, discards writes
            self.files[name] = bytearray()
        exists = name in self.files
        if disposition == 3 and not exists:
            self.last_error = 2
            return 0xFFFFFFFFFFFFFFFF
        if disposition == 2:
            self.files[name] = bytearray()
        if disposition == 4 and not exists:
            self.files[name] = bytearray()
        handle = 0x1A4 + 4 * len(self.open_files)
        inheritable = bool(sec) and struct.unpack("<IxxxxQI", bytes(self.uc.mem_read(sec, 20))) [2] == 1
        while handle in self.open_files:
            handle += 4
        self.open_files[handle] = dict(name=name, pos=0, append=(access == 4), write=bool(access & 0x40000000 or access == 4), inheritable=inheritable)
        return handle

    def api_ReadFile(self, a, s):
        handle, buf, n, got = a
        if s(4) != 0:
            self._violation("lpOverlapped must be NULL")
        if handle == 0:  # stdin behaves like a pipe: EOF is ERROR_BROKEN_PIPE
            if self.stdin_pos >= len(self.stdin):
                self.last_error = 109
                return 0
            data = self.stdin[self.stdin_pos:self.stdin_pos + n]
            self.stdin_pos += len(data)
            self.uc.mem_write(buf, bytes(data))
            self.uc.mem_write(got, struct.pack("<I", len(data)))
            return 1
        f = self.open_files.get(handle)
        if f is None:
            self.last_error = 6
            return 0
        data = self.files[f["name"]][f["pos"]:f["pos"] + n]
        f["pos"] += len(data)
        self.uc.mem_write(buf, bytes(data))
        self.uc.mem_write(got, struct.pack("<I", len(data)))
        return 1

    def api_CloseHandle(self, a, s):
        if a[0] in self.processes:
            return 1
        if a[0] in self.open_files:
            del self.open_files[a[0]]
            return 1
        self.last_error = 6
        return 0

    def api_GetLastError(self, a, s):
        return self.last_error

    # ---- directories ----
    def _norm(self, name):
        if name.startswith("/dev/"):
            return name
        name = name.replace("\\", "/")
        while name.startswith("./"):
            name = name[2:]
        return name.lstrip("/")

    def _children(self, directory):
        directory = self._norm(directory).rstrip("/")
        prefix = directory + "/" if directory else ""
        seen = {}
        for name in self.files:
            n = self._norm(name)
            if n.startswith(prefix) and n != directory:
                rest = n[len(prefix):]
                head = rest.split("/")[0]
                seen[head] = seen.get(head, False) or "/" in rest
        for d in self.dirs:
            n = self._norm(d)
            if n.startswith(prefix) and n != directory:
                head = n[len(prefix):].split("/")[0]
                seen[head] = True
        return sorted(seen.items())

    def _is_dir(self, name):
        n = self._norm(name).rstrip("/")
        return n in {self._norm(d).rstrip("/") for d in self.dirs} or any(self._norm(f).startswith(n + "/") for f in self.files)

    def _fill_find(self, buf, entry):
        name, is_dir = entry
        self.uc.mem_write(buf, struct.pack("<I", 0x10 if is_dir else 0x80) + b"\0" * 40)
        wide = (name + "\0").encode("utf-16-le")
        self.uc.mem_write(buf + 44, wide + b"\0" * (520 - len(wide)))

    def _read_wide(self, ptr):
        raw = b""
        while True:
            c = bytes(self.uc.mem_read(ptr + len(raw), 2))
            if c == b"\0\0":
                return raw.decode("utf-16-le")
            raw += c

    def _canonical(self, name):
        """Map a path as the program spells it onto the key used in self.files (normalized spelling)."""
        n = self._norm(name)
        for key in self.files:
            if self._norm(key) == n:
                return key
        return n

    def api_GetFileAttributesW(self, a, s):
        name = self._read_wide(a[0])
        if self._is_dir(name):
            return 0x10
        if name in self.files or self._norm(name) in {self._norm(f) for f in self.files}:
            return 0x80
        self.last_error = 2
        return 0xFFFFFFFF

    def api_CreateDirectoryW(self, a, s):
        name = self._read_wide(a[0])
        if self._is_dir(name) or name in self.files:
            self.last_error = 183
            return 0
        self.dirs.add(name)
        return 1

    def api_FindFirstFileW(self, a, s):
        pattern = self._read_wide(a[0])
        if not pattern.endswith("*"):
            self._violation("FindFirstFileW pattern must end in *: %r" % pattern)
        directory = pattern[:-1].rstrip("\\/")
        if not self._is_dir(directory) and directory not in ("", "."):
            self.last_error = 3
            return 0xFFFFFFFFFFFFFFFF
        entries = [(".", True), ("..", True)] + self._children("" if directory in ("", ".") else directory)
        handle = 0x7000 + 8 * len(self.finds)
        self.finds[handle] = [entries, 1]
        self._fill_find(a[1], entries[0])
        return handle

    def api_FindNextFileW(self, a, s):
        f = self.finds.get(a[0])
        if f is None:
            self.last_error = 6
            return 0
        if f[1] >= len(f[0]):
            self.last_error = 18
            return 0
        self._fill_find(a[1], f[0][f[1]])
        f[1] += 1
        return 1

    def api_FindClose(self, a, s):
        return 1 if self.finds.pop(a[0], None) is not None else 0

    def api_WideCharToMultiByte(self, a, s):
        codepage, flags, src, n = a[0] & 0xFFFFFFFF, a[1] & 0xFFFFFFFF, a[2], a[3] & 0xFFFFFFFF
        dst, cap, default, used = s(4), s(5) & 0xFFFFFFFF, s(6), s(7)
        if codepage != 65001 or n != 0xFFFFFFFF or default or used:
            self._violation("unexpected WideCharToMultiByte arguments")
        data = (self._read_wide(src) + "\0").encode("utf-8")
        if len(data) > cap:
            self.last_error = 122
            return 0
        self.uc.mem_write(dst, data)
        return len(data)

    # ---- process environment ----
    def _wide_alloc(self, text_or_list):
        data = b"".join((t + "\0").encode("utf-16-le") for t in text_or_list) if isinstance(text_or_list, list) else (text_or_list + "\0").encode("utf-16-le")
        data += b"\0\0" if isinstance(text_or_list, list) else b""
        base = self.next_region
        self.next_region += (len(data) + 0xFFFF) & ~0xFFFF
        self.uc.mem_map(base, (len(data) + 0xFFF) & ~0xFFF, UC_PROT_ALL)
        self.uc.mem_write(base, data)
        return base

    def api_GetCommandLineW(self, a, s):
        return self._wide_alloc(self.command_line)

    def api_GetEnvironmentStringsW(self, a, s):
        return self._wide_alloc(["%s=%s" % kv for kv in self.environment.items()])

    def api_DeleteFileW(self, a, s):
        name = self._canonical(self._read_wide(a[0]))
        if name in self.files:
            del self.files[name]
            return 1
        self.last_error = 2
        return 0

    def api_RemoveDirectoryW(self, a, s):
        name = self._read_wide(a[0])
        if self._is_dir(name) and not self._children(name):
            self.dirs.discard(name)
            return 1
        self.last_error = 2
        return 0

    def api_GetCurrentProcessId(self, a, s):
        return 4242

    def api_GetTickCount64(self, a, s):
        self.ticks += 1
        return self.ticks

    def api_GetFullPathNameW(self, a, s):
        name = self._read_wide(a[0])
        full = "C:\\work\\" + self._norm(name).replace("/", "\\") if not (len(name) > 1 and name[1] == ":") else name
        full = full.rstrip("\\.") if name in (".", "") else full
        wide = (full + "\0").encode("utf-16-le")
        if len(wide) // 2 > a[1] & 0xFFFFFFFF:
            return len(wide) // 2
        self.uc.mem_write(a[2], wide)
        return len(full)

    # ---- processes (model: the child image runs to completion inside CreateProcessW, from the same file table) ----
    def api_CreateProcessW(self, a, s):
        app = self._canonical(self._read_wide(a[0])) if a[0] else None
        command_line = self._read_wide(a[1])
        if a[2] or a[3] or s(5) or s(6) or s(7):
            self._violation("unexpected CreateProcessW security/flags/environment/directory arguments")
        inherit, info, pinfo = s(4) & 0xFFFFFFFF, s(8), s(9)
        if app is None or app not in self.files:
            self.last_error = 2
            return 0
        cb = struct.unpack("<I", bytes(self.uc.mem_read(info, 4)))[0]
        flags, cb2, reserved2 = struct.unpack("<IxxH4xQ", bytes(self.uc.mem_read(info + 60, 20)))
        stdin, stdout, stderr = struct.unpack("<QQQ", bytes(self.uc.mem_read(info + 80, 24)))
        if cb != 104:
            self._violation("STARTUPINFOW.cb must be 104")
        child = Emu(bytes(self.files[app]), files=self.files, command_line=command_line, environment=self.environment,
                    max_instructions=self.child_instructions)
        child.child_instructions = self.child_instructions
        if flags & 0x100:
            if not inherit:
                self._violation("STARTF_USESTDHANDLES without inheritance")
            for std, handle in ((-11, stdout), (-12, stderr)):
                entry = self.open_files.get(handle)
                if entry is None or not entry.get("inheritable"):
                    self._violation("redirected handle %#x is not an inheritable open file" % handle)
                    continue
                child_handle = 0x9000 + 4 * len(child.open_files)
                child.open_files[child_handle] = dict(entry, pos=0)
                child.handles[std & 0xFFFFFFFF] = child_handle
        if cb2:
            table = bytes(self.uc.mem_read(reserved2, cb2))
            count = struct.unpack_from("<I", table, 0)[0]
            for fd in range(count):
                handle = struct.unpack_from("<Q", table, 4 + count + 8 * fd)[0]
                if table[4 + fd] & 1:
                    entry = self.open_files.get(handle)
                    if entry is None or not entry.get("inheritable"):
                        self._violation("lpReserved2 handle %#x is not an inheritable open file" % handle)
                        continue
                    child.open_files[handle] = dict(entry, pos=0)
            child.reserved2 = table
        code, out, err, violations = child.run()
        self.violations += ["child: " + v for v in violations if v != "did not exit"]
        self.out += out
        self.err += err
        process = 0x7000 + 8 * len(self.processes)
        self.processes[process] = None if code is None else code
        self.uc.mem_write(pinfo, struct.pack("<QQII", process, process + 4, 100, 101))
        self.processes[process + 4] = "thread"
        return 1

    def api_WaitForSingleObject(self, a, s):
        handle, timeout = a[0], a[1] & 0xFFFFFFFF
        if handle in self.processes:
            if self.processes[handle] is None:  # never exited within the instruction budget: a timed-out child
                if timeout == 0xFFFFFFFF:
                    self._violation("infinite wait on a child that never exits")
                return 258
            return 0
        if handle == 0:
            return 0 if self.stdin_pos < len(self.stdin) else 258
        self.last_error = 6
        return 0xFFFFFFFF

    def api_TerminateProcess(self, a, s):
        if a[0] not in self.processes:
            self.last_error = 6
            return 0
        self.processes[a[0]] = a[1] & 0xFFFFFFFF
        return 1

    def api_GetExitCodeProcess(self, a, s):
        code = self.processes.get(a[0])
        if code is None or code == "thread":
            self.last_error = 6
            return 0
        self.uc.mem_write(a[1], struct.pack("<I", code))
        return 1

    def api_GetStartupInfoW(self, a, s):
        info = bytearray(104)
        struct.pack_into("<I", info, 0, 104)
        if self.reserved2:
            base = self.next_region
            self.next_region += 0x10000
            self.uc.mem_map(base, 0x1000, UC_PROT_ALL)
            self.uc.mem_write(base, self.reserved2)
            struct.pack_into("<HQ", info, 66, len(self.reserved2), 0)
            struct.pack_into("<Q", info, 72, base)
        self.uc.mem_write(a[0], bytes(info))
        return 0

    def api_GetFileType(self, a, s):
        if a[0] == 0:
            return 3
        return 1 if a[0] in self.open_files else 0

    def api_PeekNamedPipe(self, a, s):
        if a[0] != 0:
            self.last_error = 6
            return 0
        if self.stdin_pos >= len(self.stdin):
            self.last_error = 109
            return 0
        if s(4):
            self.uc.mem_write(s(4), struct.pack("<I", len(self.stdin) - self.stdin_pos))
        return 1

    def api_Sleep(self, a, s):
        self.ticks += a[0] & 0xFFFFFFFF
        return 0

    def api_GetModuleFileNameW(self, a, s):
        if a[0] != 0:
            self._violation("GetModuleFileNameW expects the current module (NULL)")
        path = "C:\\work\\" + self.command_line.split(" ")[0].strip('"')
        wide = (path + "\0").encode("utf-16-le")
        if len(wide) // 2 > (a[2] & 0xFFFFFFFF):
            self.last_error = 122
            return a[2] & 0xFFFFFFFF
        self.uc.mem_write(a[1], wide)
        self.files.setdefault(self._canonical(path), bytearray(b"MZ"))
        return len(path)

    def api_ExitProcess(self, a, s):
        self.exit_code = a[0] & 0xFFFFFFFF
        return 0

    def run(self):
        try:
            self.uc.emu_start(self.entry, 0, count=self.max_instructions)
        except UcError as error:
            self._violation("CPU fault: %s at rip=%#x" % (error, self.reg(UC_X86_REG_RIP)))
        if self.exit_code is None:
            self._violation("did not exit")
        return self.exit_code, bytes(self.out), bytes(self.err), self.violations


if __name__ == "__main__":
    e = Emu(open(sys.argv[1], "rb").read())
    code, out, err, v = e.run()
    sys.stdout.buffer.write(out)
    sys.stderr.buffer.write(err)
    print("exit=%s violations=%s" % (code, v), file=sys.stderr)
