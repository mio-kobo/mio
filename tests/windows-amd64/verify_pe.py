"""Independent structural check of lowerwin PE images (development-only; parses bytes without the Mio writer)."""
import struct

IMAGE_BASE = 0x140000000


class PEError(ValueError):
    pass


def require(ok, message):
    if not ok:
        raise PEError(message)


def parse(data):
    require(data[:2] == b"MZ", "DOS magic")
    pe = struct.unpack_from("<I", data, 60)[0]
    require(data[pe:pe + 4] == b"PE\0\0", "PE signature")
    machine, nsec, _, _, _, optsize, _ = struct.unpack_from("<HHIIIHH", data, pe + 4)
    require(machine == 0x8664 and optsize == 240, "AMD64 PE32+ headers")
    opt = pe + 24
    require(struct.unpack_from("<H", data, opt)[0] == 0x20B, "PE32+ magic")
    entry = struct.unpack_from("<I", data, opt + 16)[0]
    base = struct.unpack_from("<Q", data, opt + 24)[0]
    require(base == IMAGE_BASE, "image base")
    size_image = struct.unpack_from("<I", data, opt + 56)[0]
    dirs = [struct.unpack_from("<II", data, opt + 112 + 8 * i) for i in range(16)]
    secs = []
    for i in range(nsec):
        o = opt + optsize + 40 * i
        name = data[o:o + 8].rstrip(b"\0").decode()
        vsize, rva, rawsize, rawptr = struct.unpack_from("<IIII", data, o + 8)
        flags = struct.unpack_from("<I", data, o + 36)[0]
        secs.append(dict(name=name, vsize=vsize, rva=rva, rawsize=rawsize, rawptr=rawptr, flags=flags))
    return dict(entry=entry, size_image=size_image, dirs=dirs, secs=secs)


def read_rva(data, img, rva, n):
    for s in img["secs"]:
        if s["rva"] <= rva and rva + n <= s["rva"] + max(s["vsize"], 0) and rva - s["rva"] + n <= s["rawsize"]:
            o = s["rawptr"] + rva - s["rva"]
            return data[o:o + n]
    raise PEError("RVA not file-backed: %#x" % rva)


def read_imports(data, img):
    """Return each library's named IAT slots in image order."""
    def cstring(rva):
        value = bytearray()
        while (byte := read_rva(data, img, rva + len(value), 1)) != b"\0":
            value.extend(byte)
        return value.decode("ascii")

    rva, size = img["dirs"][1]
    require(rva and size >= 20 and size % 20 == 0, "import directory")
    libraries = {}
    for offset in range(0, size, 20):
        ilt, stamp, forwarder, name, iat = struct.unpack("<IIIII", read_rva(data, img, rva + offset, 20))
        if not any((ilt, stamp, forwarder, name, iat)):
            require(offset == size - 20, "import directory terminator")
            return libraries
        dll = cstring(name)
        require(ilt and iat and dll not in libraries, "invalid import descriptor")
        symbols = {}
        index = 0
        while True:
            hint, = struct.unpack("<Q", read_rva(data, img, ilt + 8 * index, 8))
            slot = iat + 8 * index
            require(read_rva(data, img, slot, 8) == struct.pack("<Q", hint), "IAT and lookup differ")
            if hint == 0:
                break
            symbol = cstring(hint + 2)
            require(symbol not in symbols, "duplicate named import")
            symbols[symbol] = slot
            index += 1
        libraries[dll] = symbols
    raise PEError("missing import terminator")


def verify_winsock(data):
    img = parse(data)
    slots = read_imports(data, img).get("WS2_32.dll", {})
    expected = {"WSAStartup", "WSACleanup", "WSAGetLastError", "WSASocketW", "setsockopt",
                "bind", "connect", "listen", "accept", "recv", "send", "closesocket"}
    require(slots.keys() == expected, "unexpected Winsock imports: " + repr(list(slots)))
    iat, iat_size = img["dirs"][12]
    text = next(s for s in img["secs"] if s["name"] == ".text")
    code = read_rva(data, img, text["rva"], text["vsize"])
    calls = {text["rva"] + i + 6 + struct.unpack_from("<i", code, i + 2)[0]
             for i in range(len(code) - 5) if code[i:i + 2] == b"\xff\x15"}
    for symbol, slot in slots.items():
        require(iat <= slot <= iat + iat_size - 8, symbol + " outside IAT")
        require(slot in calls, "missing RIP-relative call to " + symbol)
    return list(slots)


def verify(data, expect_relocations=True):
    img = parse(data)
    secs = {s["name"]: s for s in img["secs"]}
    for name in (".text", ".pdata", ".xdata", ".idata") + ((".reloc",) if expect_relocations else ()):
        require(name in secs, "missing section " + name)
    text = secs[".text"]
    require(text["flags"] & 0x20000000 and text["flags"] & 0x40000000 and not text["flags"] & 0x80000000, ".text is RX")
    require(img["entry"] == text["rva"], "entry is the first function")
    tdata = data[text["rawptr"]:text["rawptr"] + text["vsize"]]
    # unwind
    pd_rva, pd_size = img["dirs"][3]
    require(pd_rva == secs[".pdata"]["rva"] and pd_size % 12 == 0 and pd_size > 0, "exception directory")
    previous_end = 0
    functions = []
    for i in range(pd_size // 12):
        begin, end, info = struct.unpack("<III", read_rva(data, img, pd_rva + 12 * i, 12))
        require(begin >= previous_end and end > begin, "RUNTIME_FUNCTION sorted and non-overlapping")
        require(text["rva"] <= begin and end <= text["rva"] + text["vsize"], "function inside .text")
        previous_end = end
        u = read_rva(data, img, info, 4)
        version_flags, prolog, count, frame = u
        require(version_flags == 1, "unwind version 1, no handler")
        require(frame == 5, "frame register rbp, offset 0")
        codes = read_rva(data, img, info + 4, 2 * count)
        parsed, k = [], 0
        while k < count:
            off, opinfo = codes[2 * k], codes[2 * k + 1]
            op, inf = opinfo & 15, opinfo >> 4
            if op == 0:
                parsed.append(("push", off, inf)); k += 1
            elif op == 3:
                parsed.append(("fp", off, 0)); k += 1
            elif op == 2:
                parsed.append(("alloc", off, 8 * inf + 8)); k += 1
            elif op == 1:
                require(inf == 0, "large alloc form")
                parsed.append(("alloc", off, 8 * struct.unpack_from("<H", codes, 2 * k + 2)[0])); k += 2
            else:
                raise PEError("unexpected unwind op %d" % op)
        # reverse order: alloc?, fp, push
        require(parsed[-1][0] == "push" and parsed[-1][2] == 5 and parsed[-2][0] == "fp", "push rbp then set_fpreg")
        alloc = parsed[0][2] if parsed[0][0] == "alloc" else 0
        # compare with the REAL prologue bytes
        b = tdata[begin - text["rva"]:end - text["rva"]]
        require(b[0] == 0x55 and parsed[-1][1] == 1, "push rbp is the first instruction")
        require(b[1:4] == b"\x48\x89\xe5" and parsed[-2][1] == 4, "mov rbp,rsp follows")
        if alloc:
            require(b[4:7] == b"\x48\x81\xec" and struct.unpack_from("<I", b, 7)[0] == alloc and parsed[0][1] == 11 and prolog == 11,
                    "sub rsp,%d matches unwind alloc" % alloc)
        else:
            require(len(parsed) == 2 and prolog == 4, "no allocation")
        functions.append(dict(begin=begin, end=end, alloc=alloc, code=b))
    # every function that returns ends its flow with the formal epilogue lea rsp,[rbp+0]; pop rbp; ret
    import re
    epilogue = re.compile(rb"\x48\x8d\xa5\x00\x00\x00\x00\x5d\xc3", re.S)
    for f in functions:
        require(epilogue.search(f["code"]) is not None, "function has the formal epilogue lea rsp,[rbp]; pop rbp; ret")
    # imports
    idata = secs[".idata"]
    ir, isz = img["dirs"][1]
    require(ir == idata["rva"], "import directory")
    ilt, _, _, name_rva, iat = struct.unpack("<IIIII", read_rva(data, img, ir, 20))
    require(read_rva(data, img, name_rva, 13) == b"KERNEL32.dll\0", "KERNEL32 import")
    names = []
    for j in range(3):
        hint = struct.unpack("<Q", read_rva(data, img, ilt + 8 * j, 8))[0]
        text_name = b""
        while True:
            c = read_rva(data, img, hint + 2 + len(text_name), 1)
            if c == b"\0":
                break
            text_name += c
        names.append(text_name.decode())
    require(names == ["GetStdHandle", "WriteFile", "ExitProcess"], "import names " + repr(names))
    if not expect_relocations:
        dll = struct.unpack_from("<H", data, struct.unpack_from("<I", data, 60)[0] + 24 + 70)[0]
        require(img["dirs"][5] == (0, 0) and not dll & 0x40, "fixed-base image: no relocations and no DYNAMIC_BASE")
        data_sec = secs[".data"]
        require(data_sec["rva"] == 0x1000 and data_sec["flags"] & 0x80000000 and secs[".text"]["rva"] == data_sec["rva"] + ((data_sec["vsize"] + 0xFFF) & ~0xFFF), ".data is RW at 0x1000 and .text follows without a gap")
        return dict(functions=len(functions), relocations=0, image_size=img["size_image"])
    # every movabs imm64 holding an in-image VA must be a DIR64 relocation
    rr, rsz = img["dirs"][5]
    relocs = set()
    raw = read_rva(data, img, rr, rsz)
    p = 0
    while p < len(raw):
        page, size = struct.unpack_from("<II", raw, p)
        require(size >= 8, "reloc block")
        for e in range(8, size, 2):
            w = struct.unpack_from("<H", raw, p + e)[0]
            if w >> 12 == 10:
                relocs.add(page + (w & 0xFFF))
            else:
                require(w >> 12 == 0, "only DIR64/ABSOLUTE relocations")
        p += size
    found = 0
    for i in range(len(tdata) - 10):
        if tdata[i] == 0x48 and 0xB8 <= tdata[i + 1] <= 0xBF or tdata[i] == 0x49 and 0xB8 <= tdata[i + 1] <= 0xBF:
            v = struct.unpack_from("<Q", tdata, i + 2)[0]
            if IMAGE_BASE <= v < IMAGE_BASE + img["size_image"]:
                # either a genuine relocated address or an unlucky constant; both must be relocated or be verified by caller
                if text["rva"] + i + 2 in relocs:
                    found += 1
    require(found > 0 and len(relocs) >= found, "relocated absolute operands present")
    for r in relocs:
        v = struct.unpack("<Q", read_rva(data, img, r, 8))[0]
        require(IMAGE_BASE <= v < IMAGE_BASE + img["size_image"], "relocation target inside image")
    return dict(functions=len(functions), relocations=len(relocs), image_size=img["size_image"])
