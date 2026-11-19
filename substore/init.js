// sub_store\init.js
// package substore

// Web API Polyfill
(function (g) {
    const _b64c = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
    const _b64d = new Uint8Array(256);
    _b64d.fill(255);
    for (let i = 0; i < _b64c.length; i++) _b64d[_b64c.charCodeAt(i)] = i;

    function _bytesToLatin1(u8) {
        const len = u8.length;
        if (len === 0) return "";
        if (len <= 16384) return String.fromCharCode.apply(null, u8);
        const parts = [];
        for (let i = 0; i < len; i += 16384) {
            parts.push(String.fromCharCode.apply(null, u8.subarray(i, Math.min(i + 16384, len))));
        }
        return parts.join("");
    }

    function _toTightUint8Array(arr) {
        if (arr instanceof Uint8Array) {
            if (arr.byteOffset === 0 && arr.byteLength === arr.buffer.byteLength) return arr;
            return new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength).slice();
        }
        if (arr instanceof ArrayBuffer) return new Uint8Array(arr);
        if (ArrayBuffer.isView(arr)) return new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength).slice();
        return new Uint8Array(arr);
    }

    if (!g.btoa) {
        g.btoa = function btoa(str) {
            str = String(str);
            const len = str.length;
            if (len === 0) return "";
            if (len > 256 && typeof __go_b64_encode === 'function') {
                const u8 = new Uint8Array(len);
                for (let i = 0; i < len; i++) {
                    const c = str.charCodeAt(i);
                    if (c > 0xff) throw new Error("InvalidCharacterError: btoa failed");
                    u8[i] = c;
                }
                return __go_b64_encode(u8);
            }
            const out = [];
            let i = 0;
            const limit = len - (len % 3);
            while (i < limit) {
                const c1 = str.charCodeAt(i++);
                const c2 = str.charCodeAt(i++);
                const c3 = str.charCodeAt(i++);
                if ((c1 | c2 | c3) > 0xff) throw new Error("InvalidCharacterError: btoa failed");
                const p = (c1 << 16) | (c2 << 8) | c3;
                out.push(_b64c.charAt((p >> 18) & 63), _b64c.charAt((p >> 12) & 63), _b64c.charAt((p >> 6) & 63), _b64c.charAt(p & 63));
            }
            if (i < len) {
                const c1 = str.charCodeAt(i++);
                if (c1 > 0xff) throw new Error("InvalidCharacterError: btoa failed");
                if (i < len) {
                    const c2 = str.charCodeAt(i);
                    if (c2 > 0xff) throw new Error("InvalidCharacterError: btoa failed");
                    const p = (c1 << 16) | (c2 << 8);
                    out.push(_b64c.charAt((p >> 18) & 63), _b64c.charAt((p >> 12) & 63), _b64c.charAt((p >> 6) & 63), "=");
                } else {
                    const p = c1 << 16;
                    out.push(_b64c.charAt((p >> 18) & 63), _b64c.charAt((p >> 12) & 63), "==", "");
                }
            }
            return out.join("");
        };
    }

    if (!g.atob) {
        g.atob = function atob(b64) {
            b64 = String(b64);
            if (b64.length === 0) return "";
            if (b64.length > 256 && typeof __go_b64_decode === 'function') {
                const u8 = __go_b64_decode(b64);
                if (u8 instanceof Uint8Array) return _bytesToLatin1(u8);
            }
            const clean = b64.replace(/[\t\n\f\r ]+/g, '');
            const len = clean.length;
            if (len === 0) return "";
            let bufLen = (len * 3) >> 2;
            if (clean.charCodeAt(len - 1) === 61) bufLen--;
            if (len > 1 && clean.charCodeAt(len - 2) === 61) bufLen--;
            const u8 = new Uint8Array(bufLen);
            let i = 0, j = 0;
            while (i < len) {
                const c1 = _b64d[clean.charCodeAt(i++)];
                const c2 = _b64d[clean.charCodeAt(i++)];
                const ch3 = clean.charCodeAt(i++);
                const ch4 = clean.charCodeAt(i++);
                const c3 = ch3 === 61 ? 0 : _b64d[ch3];
                const c4 = ch4 === 61 ? 0 : _b64d[ch4];
                const p = (c1 << 18) | (c2 << 12) | (c3 << 6) | c4;
                u8[j++] = (p >> 16) & 255;
                if (j < bufLen) u8[j++] = (p >> 8) & 255;
                if (j < bufLen) u8[j++] = p & 255;
            }
            return _bytesToLatin1(u8);
        };
    }

    if (!g.TextEncoder) {
        g.TextEncoder = class TextEncoder {
            encode(str) {
                if (str === undefined || str === null) return new Uint8Array(0);
                str = String(str);
                const len = str.length;
                if (len === 0) return new Uint8Array(0);

                // 长文本直接走 Go 层 UTF-8 编码（返回 Uint8Array，无 Base64 开销）
                if (len > 256 && typeof __go_string_to_utf8 === 'function') {
                    const res = __go_string_to_utf8(str);
                    if (res instanceof Uint8Array) return res;
                }

                // 短字符串 ASCII 快速路径（覆盖节点密码、域名、UUID、Cipher 等绝大多数调用）
                const u8 = new Uint8Array(len);
                let i = 0;
                for (; i < len; i++) {
                    const c = str.charCodeAt(i);
                    if (c >= 0x80) break;
                    u8[i] = c;
                }
                if (i === len) return u8;

                // 含非 ASCII 的短字符串回退完整 UTF-8 编码
                const arr = new Array(i);
                for (let j = 0; j < i; j++) arr[j] = u8[j];
                for (; i < len; i++) {
                    let code = str.charCodeAt(i);
                    if (code < 0x80) arr.push(code);
                    else if (code < 0x800) arr.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f));
                    else if (code < 0xd800 || code >= 0xe000) {
                        arr.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f));
                    } else {
                        i++;
                        code = 0x10000 + (((code & 0x3ff) << 10) | (str.charCodeAt(i) & 0x3ff));
                        arr.push(0xf0 | (code >> 18), 0x80 | ((code >> 12) & 0x3f), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f));
                    }
                }
                return new Uint8Array(arr);
            }
        };
    }

    if (!g.TextDecoder) {
        g.TextDecoder = class TextDecoder {
            decode(arr) {
                if (!arr) return "";
                if (arr.__orig_b64 && typeof __go_base64_to_string === 'function') {
                    return __go_base64_to_string(arr.__orig_b64);
                }

                let u8 = arr;
                if (arr instanceof ArrayBuffer) {
                    u8 = new Uint8Array(arr);
                } else if (ArrayBuffer.isView(arr) && !(arr instanceof Uint8Array)) {
                    u8 = new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength);
                }

                const len = u8.length;
                if (!len) return "";

                // 长数据直接走 Go 层 UTF-8 解码（无 Base64 开销）
                if (len > 256 && typeof __go_utf8_to_string === 'function') {
                    const s = __go_utf8_to_string(_toTightUint8Array(u8));
                    if (typeof s === 'string') return s;
                }

                // 短 ASCII 快速解码
                let isAscii = true;
                for (let i = 0; i < len; i++) {
                    if (u8[i] >= 0x80) {
                        isAscii = false;
                        break;
                    }
                }
                if (isAscii) {
                    return _bytesToLatin1(u8);
                }

                const out = [];
                for (let i = 0; i < len; i++) {
                    const c = u8[i];
                    if (c < 0x80) out.push(c);
                    else if (c > 0xbf && c < 0xe0) {
                        out.push(((c & 0x1f) << 6) | (u8[++i] & 0x3f));
                    } else if (c > 0xdf && c < 0xf0) {
                        out.push(((c & 0x0f) << 12) | ((u8[++i] & 0x3f) << 6) | (u8[++i] & 0x3f));
                    } else {
                        const code = (((c & 0x07) << 18) | ((u8[++i] & 0x3f) << 12) | ((u8[++i] & 0x3f) << 6) | (u8[++i] & 0x3f)) - 0x10000;
                        out.push(0xd800 | (code >> 10), 0xdc00 | (code & 0x3ff));
                    }
                }
                return String.fromCharCode.apply(null, out);
            }
        };
    }

    if (!g.crypto) g.crypto = {};
    if (!g.crypto.getRandomValues) {
        g.crypto.getRandomValues = function (arr) {
            const byteLen = arr.byteLength;
            if (byteLen === 0) return arr;
            if (typeof __go_crypto_random_bytes === 'function') {
                const randU8 = __go_crypto_random_bytes(byteLen);
                if (randU8 instanceof Uint8Array) {
                    new Uint8Array(arr.buffer, arr.byteOffset, byteLen).set(randU8);
                    return arr;
                }
            }
            const u8 = new Uint8Array(arr.buffer, arr.byteOffset, byteLen);
            for (let i = 0; i < byteLen; i++) u8[i] = (Math.random() * 256) | 0;
            return arr;
        };
    }

    function _concatChunks(chunks) {
        if (chunks.length === 0) return new Uint8Array(0);
        if (chunks.length === 1) return chunks[0];
        let total = 0;
        for (let i = 0; i < chunks.length; i++) total += chunks[i].length;
        const out = new Uint8Array(total);
        let offset = 0;
        for (let i = 0; i < chunks.length; i++) {
            out.set(chunks[i], offset);
            offset += chunks[i].length;
        }
        return out;
    }

    if (!g.ReadableStream) {
        g.ReadableStream = class ReadableStream {
            constructor(underlyingSource = {}) {
                this._chunks = [];
                this._closed = false;
                const controller = {
                    enqueue: (chunk) => {
                        this._chunks.push(chunk instanceof Uint8Array ? chunk : _toTightUint8Array(chunk));
                    },
                    close: () => {
                        this._closed = true;
                    }
                };
                if (typeof underlyingSource.start === 'function') {
                    underlyingSource.start(controller);
                }
            }
            getReader() {
                let idx = 0;
                const self = this;
                return {
                    async read() {
                        if (idx < self._chunks.length) {
                            return { value: self._chunks[idx++], done: false };
                        }
                        return { value: undefined, done: true };
                    },
                    releaseLock() { }
                };
            }
            async pipeTo(writable) {
                const writer = writable.getWriter();
                for (let i = 0; i < this._chunks.length; i++) {
                    await writer.write(this._chunks[i]);
                }
                await writer.close();
            }
            pipeThrough(transform) {
                const promise = this.pipeTo(transform.writable);
                if (transform.readable) {
                    transform.readable._pendingPipe = promise;
                }
                return transform.readable;
            }
        };
    }

    if (!g.TransformStream) {
        g.TransformStream = class TransformStream {
            constructor() {
                const chunks = [];
                this.readable = new g.ReadableStream({
                    start: (ctrl) => { this._ctrl = ctrl; }
                });
                this.writable = {
                    getWriter: () => ({
                        write: async (chunk) => { chunks.push(_toTightUint8Array(chunk)); },
                        close: async () => { this._ctrl.enqueue(_concatChunks(chunks)); this._ctrl.close(); }
                    })
                };
            }
        };
    }

    if (!g.CompressionStream) {
        g.CompressionStream = class CompressionStream {
            constructor(format) {
                const chunks = [];
                let ctrl;
                this.readable = new g.ReadableStream({
                    start: (c) => { ctrl = c; }
                });
                this.writable = {
                    getWriter: () => ({
                        write: async (chunk) => {
                            if (typeof chunk === 'string') {
                                chunks.push(new g.TextEncoder().encode(chunk));
                            } else {
                                chunks.push(_toTightUint8Array(chunk));
                            }
                        },
                        close: async () => {
                            const merged = _concatChunks(chunks);
                            const out = typeof __go_compress === 'function' ? __go_compress(String(format), merged) : merged;
                            ctrl.enqueue(out);
                            ctrl.close();
                        }
                    })
                };
            }
        };
    }

    if (!g.DecompressionStream) {
        g.DecompressionStream = class DecompressionStream {
            constructor(format) {
                const chunks = [];
                let ctrl;
                this.readable = new g.ReadableStream({
                    start: (c) => { ctrl = c; }
                });
                this.writable = {
                    getWriter: () => ({
                        write: async (chunk) => {
                            chunks.push(_toTightUint8Array(chunk));
                        },
                        close: async () => {
                            const merged = _concatChunks(chunks);
                            const out = typeof __go_decompress === 'function' ? __go_decompress(String(format), merged) : merged;
                            ctrl.enqueue(out);
                            ctrl.close();
                        }
                    })
                };
            }
        };
    }

    if (!g.Blob) {
        g.Blob = class Blob {
            constructor(parts = []) {
                const encoder = new g.TextEncoder();
                const chunks = [];
                for (let i = 0; i < parts.length; i++) {
                    const p = parts[i];
                    if (typeof p === 'string') chunks.push(encoder.encode(p));
                    else chunks.push(_toTightUint8Array(p));
                }
                this._data = _concatChunks(chunks);
            }
            stream() {
                const data = this._data;
                return new g.ReadableStream({
                    start(ctrl) {
                        if (data.length > 0) ctrl.enqueue(data);
                        ctrl.close();
                    }
                });
            }
            async arrayBuffer() {
                return this._data.buffer.slice(this._data.byteOffset, this._data.byteOffset + this._data.byteLength);
            }
            async text() {
                return new g.TextDecoder().decode(this._data);
            }
        };
    }

    if (!g.Response) {
        g.Response = class Response {
            constructor(body = null) {
                this.body = body;
            }
            async bytes() {
                if (!this.body) return new Uint8Array(0);
                if (this.body instanceof Uint8Array) return this.body;
                if (typeof this.body === 'string') return new g.TextEncoder().encode(this.body);
                if (this.body._pendingPipe) await this.body._pendingPipe;
                if (typeof this.body.getReader === 'function') {
                    const reader = this.body.getReader();
                    const chunks = [];
                    while (true) {
                        const { value, done } = await reader.read();
                        if (done) break;
                        if (value) chunks.push(_toTightUint8Array(value));
                    }
                    return _concatChunks(chunks);
                }
                return _toTightUint8Array(this.body);
            }
            async arrayBuffer() {
                const u8 = await this.bytes();
                return u8.buffer.slice(u8.byteOffset, u8.byteOffset + u8.byteLength);
            }
            async text() {
                const u8 = await this.bytes();
                return new g.TextDecoder().decode(u8);
            }
            async json() {
                return JSON.parse(await this.text());
            }
        };
    }
})(globalThis);

// 缓存无状态 RegExp（不含 g/y 标志），消除 RegexFilter 在节点循环中重复编译同一正则的开销
(function (g) {
    const OrigRegExp = g.RegExp;
    const reCache = new Map();
    let cacheCount = 0;

    function CachedRegExp(pattern, flags) {
        if (!new.target && pattern instanceof OrigRegExp && flags === undefined) {
            return pattern;
        }
        if (typeof pattern === 'string') {
            const f = flags === undefined ? '' : String(flags);
            if (f.indexOf('g') === -1 && f.indexOf('y') === -1) {
                let byFlag = reCache.get(f);
                if (byFlag !== undefined) {
                    const hit = byFlag.get(pattern);
                    if (hit !== undefined) return hit;
                } else {
                    byFlag = new Map();
                    reCache.set(f, byFlag);
                }
                const re = new OrigRegExp(pattern, f);
                if (cacheCount >= 1024) {
                    reCache.clear();
                    cacheCount = 0;
                    byFlag = new Map();
                    reCache.set(f, byFlag);
                }
                byFlag.set(pattern, re);
                cacheCount++;
                return re;
            }
        }
        return new OrigRegExp(pattern, flags);
    }

    CachedRegExp.prototype = OrigRegExp.prototype;
    Object.setPrototypeOf(CachedRegExp, OrigRegExp);
    g.RegExp = CachedRegExp;
})(globalThis);

// 加速排序期间的正则重复匹配（仅在长数组排序期间临时挂载，其余时间保持原生 C 方法零包装开销）
(function () {
    const _origSort = Array.prototype.sort;
    const _origTest = RegExp.prototype.test;
    const _origSearch = String.prototype.search;

    let _sortDepth = 0;
    let _regexTestCache = null;   // Map<RegExp, Map<string, boolean> | false>
    let _regexSearchCache = null; // Map<RegExp, Map<string, number> | false>

    function _cachedTest(str) {
        if (_regexTestCache === null) {
            _regexTestCache = new Map();
        }
        let m = _regexTestCache.get(this);
        if (m === false) {
            return _origTest.call(this, str);
        }
        if (m === undefined) {
            if (!(this instanceof RegExp) || this.global || this.sticky) {
                _regexTestCache.set(this, false);
                return _origTest.call(this, str);
            }
            m = new Map();
            _regexTestCache.set(this, m);
        }
        const s = typeof str === 'string' ? str : String(str);
        let hit = m.get(s);
        if (hit !== undefined) return hit;
        hit = _origTest.call(this, s);
        m.set(s, hit);
        return hit;
    }

    function _cachedSearch(regexp) {
        if (_regexSearchCache === null) {
            _regexSearchCache = new Map();
        }
        let m = _regexSearchCache.get(regexp);
        if (m === false) {
            return _origSearch.call(this, regexp);
        }
        if (m === undefined) {
            if (!(regexp instanceof RegExp) || regexp.global || regexp.sticky) {
                _regexSearchCache.set(regexp, false);
                return _origSearch.call(this, regexp);
            }
            m = new Map();
            _regexSearchCache.set(regexp, m);
        }
        const s = typeof this === 'string' ? this : String(this);
        let hit = m.get(s);
        if (hit !== undefined) return hit;
        hit = _origSearch.call(s, regexp);
        m.set(s, hit);
        return hit;
    }

    Array.prototype.sort = function (compareFn) {
        if (typeof compareFn !== 'function' || this.length < 20) {
            return _origSort.call(this, compareFn);
        }
        const isOuter = _sortDepth === 0;
        _sortDepth++;
        if (isOuter) {
            RegExp.prototype.test = _cachedTest;
            String.prototype.search = _cachedSearch;
        }
        try {
            return _origSort.call(this, compareFn);
        } finally {
            _sortDepth--;
            if (isOuter) {
                RegExp.prototype.test = _origTest;
                String.prototype.search = _origSearch;
                _regexTestCache = null;
                _regexSearchCache = null;
            }
        }
    };
})();

// 接收 Go 层注入的常量字面量
const $loon = __LOON_JSON_OBJ__;
const $script = __SCRIPT_JSON_OBJ__;
var $request = {};
var $argument = "";

const $persistentStore = {
    read: function (key) {
        const val = __ps_read(key);
        return val === undefined ? null : val;
    },
    write: function (val, key) {
        return __ps_write(val, key) !== false;
    }
};

const $notification = {
    post: function (title, subtitle, content) {
        __notify_post(title, subtitle, content);
    }
};

// 智能日志防洪堤，限制缓存上限防止海量域名解析场景下内存膨胀
const __log_cache = new Map();
function __throttled_log(level) {
    const argc = arguments.length;
    let msg = "";
    if (argc === 2) {
        msg = String(arguments[1]);
    } else if (argc > 2) {
        const parts = new Array(argc - 1);
        for (let i = 1; i < argc; i++) parts[i - 1] = String(arguments[i]);
        msg = parts.join(" ");
    }

    const key = level + ":" + msg;
    const count = (__log_cache.get(key) || 0) + 1;
    if (__log_cache.size > 512 && !__log_cache.has(key)) {
        __log_cache.clear();
    }
    __log_cache.set(key, count);

    if (count <= 3) {
        __console_log(level, msg);
    } else if (count === 4) {
        __console_log(level, msg + " (⚠️ 该日志重复出现，已折叠隐藏...)");
    }
}

const console = {
    log: function () { __throttled_log("log", ...arguments); },
    info: function () { __throttled_log("info", ...arguments); },
    warn: function () { __throttled_log("warn", ...arguments); },
    error: function () { __throttled_log("error", ...arguments); }
};

// 仅在真正触发过 DoH AAAA->A 回退后才启用 answers 过滤兼容，平时保持原生 C filter 零包装开销
let __doh_fallback_used = false;
const _originalFilter = Array.prototype.filter;
function __enableDohFilterCompat() {
    if (__doh_fallback_used) return;
    __doh_fallback_used = true;
    Array.prototype.filter = function (fn, thisArg) {
        if (this.length > 0) {
            const first = this[0];
            if (first !== null && typeof first === 'object' && first.type !== undefined && (first.data !== undefined || first.value !== undefined)) {
                const strictRes = _originalFilter.call(this, fn, thisArg);
                if (strictRes.length > 0) return strictRes;
                const fnStr = String(fn);
                if (fnStr.indexOf('.type') !== -1 && fnStr.indexOf('===') !== -1) {
                    return _originalFilter.call(this, function (i) {
                        return i && (i.type === 'A' || i.type === 1 || i.type === 'AAAA' || i.type === 28);
                    });
                }
                return strictRes;
            }
        }
        return _originalFilter.call(this, fn, thisArg);
    };
}

// JS 侧的无损二进制/Base64转换器
const _b64chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
const _b64tab = new Uint8Array(256);
for (let i = 0; i < _b64chars.length; i++) _b64tab[_b64chars.charCodeAt(i)] = i;

function __base64_to_uint8array(b64) {
    b64 = b64.replace(/[^A-Za-z0-9\+\/\=]/g, '');
    const len = b64.length;
    if (len === 0) return new Uint8Array(0);

    let u8;
    if (len > 256 && typeof __go_b64_decode === 'function') {
        u8 = __go_b64_decode(b64);
    } else {
        let bufLen = (len * 3) / 4;
        if (b64.charAt(len - 1) === '=') bufLen--;
        if (len > 1 && b64.charAt(len - 2) === '=') bufLen--;

        u8 = new Uint8Array(bufLen);
        let i = 0, j = 0;
        while (i < len) {
            const c1 = _b64tab[b64.charCodeAt(i++)];
            const c2 = _b64tab[b64.charCodeAt(i++)];
            const c3 = _b64tab[b64.charCodeAt(i++)];
            const c4 = _b64tab[b64.charCodeAt(i++)];

            const p = (c1 << 18) | (c2 << 12) | ((c3 !== undefined ? c3 : 0) << 6) | (c4 !== undefined ? c4 : 0);
            u8[j++] = (p >> 16) & 255;
            if (j < bufLen) u8[j++] = (p >> 8) & 255;
            if (j < bufLen) u8[j++] = p & 255;
        }
    }

    const res = typeof Buffer !== 'undefined' ? Buffer.from(u8.buffer, u8.byteOffset, u8.byteLength) : u8;
    res.__orig_b64 = b64;
    return res;
}

function __uint8array_to_base64(u8) {
    const len = u8.length;
    if (len === 0) return "";
    if (len > 256 && typeof __go_b64_encode === 'function') {
        const view = (u8.byteOffset === 0 && u8.byteLength === u8.buffer.byteLength)
            ? u8
            : new Uint8Array(u8.buffer, u8.byteOffset, u8.byteLength).slice();
        return __go_b64_encode(view);
    }
    const out = [];
    let i = 0;
    const limit = len - (len % 3);
    while (i < limit) {
        const p = (u8[i++] << 16) | (u8[i++] << 8) | u8[i++];
        out.push(
            _b64chars.charAt((p >> 18) & 63),
            _b64chars.charAt((p >> 12) & 63),
            _b64chars.charAt((p >> 6) & 63),
            _b64chars.charAt(p & 63)
        );
    }
    if (i < len) {
        const c1 = u8[i++];
        if (i < len) {
            const c2 = u8[i];
            const p = (c1 << 16) | (c2 << 8);
            out.push(
                _b64chars.charAt((p >> 18) & 63),
                _b64chars.charAt((p >> 12) & 63),
                _b64chars.charAt((p >> 6) & 63),
                "="
            );
        } else {
            const p = c1 << 16;
            out.push(
                _b64chars.charAt((p >> 18) & 63),
                _b64chars.charAt((p >> 12) & 63),
                "==",
                ""
            );
        }
    }
    return out.join("");
}

const $httpClient = {};
const __http_callbacks = {};
let __http_req_id = 0;

['get', 'post', 'put', 'patch', 'delete', 'head', 'options'].forEach(method => {
    const dohMethod = method.toUpperCase();
    $httpClient[method] = function (options, callback) {
        const req = typeof options === 'string' ? { url: options } : Object.assign({}, options);

        let rawBody = null;
        if (req.body && (req.body instanceof Uint8Array || (typeof Buffer !== 'undefined' && Buffer.isBuffer(req.body)))) {
            rawBody = (req.body.byteOffset === 0 && req.body.byteLength === req.body.buffer.byteLength && req.body.constructor === Uint8Array)
                ? req.body
                : new Uint8Array(req.body.buffer, req.body.byteOffset, req.body.byteLength).slice();
            delete req.body;
        }

        const reqId = "req_" + (++__http_req_id);
        const originalCallback = callback;
        let isDoH = false, isAAAA = false, isJSONDoH = false;
        let queryBuf = null;

        // 探测 Ali / Tencent 的 JSON DoH 请求格式
        if (req.url && req.url.indexOf('223.6.6.6/resolve') !== -1 && req.url.indexOf('type=AAAA') !== -1) {
            isJSONDoH = 'ali';
        } else if (req.url && req.url.indexOf('119.28.28.28/d') !== -1 && req.url.indexOf('type=AAAA') !== -1) {
            isJSONDoH = 'tencent';
        } else {
            const accept = req.headers && (req.headers['accept'] || req.headers['Accept']);
            const ctype = req.headers && (req.headers['content-type'] || req.headers['Content-Type']);
            if (accept === 'application/dns-message' || ctype === 'application/dns-message' || (typeof req.url === 'string' && req.url.indexOf('dns-query') !== -1)) {
                isDoH = true;
            }
        }

        if (isDoH) {
            if (dohMethod === 'GET') {
                const match = req.url.match(/[\?&]dns=([^&]+)/);
                if (match) {
                    const b64url = match[1];
                    let b64 = b64url.replace(/-/g, '+').replace(/_/g, '/');
                    while (b64.length % 4) b64 += '=';
                    queryBuf = __base64_to_uint8array(b64);
                }
            } else if (dohMethod === 'POST') {
                if (rawBody) {
                    queryBuf = rawBody;
                } else if (req.body && typeof req.body === 'string') {
                    queryBuf = __base64_to_uint8array(req.body);
                }
            }
            if (queryBuf && queryBuf.length > 12) {
                let offset = 12;
                while (offset < queryBuf.length) {
                    const len = queryBuf[offset];
                    if (len === 0) { offset++; break; }
                    if ((len & 0xC0) === 0xC0) { offset += 2; break; }
                    offset += len + 1;
                }
                if (offset + 4 <= queryBuf.length) {
                    const qtype = (queryBuf[offset] << 8) | queryBuf[offset + 1];
                    if (qtype === 28) isAAAA = true;
                }
            }
        }

        if ((isDoH && isAAAA) || isJSONDoH) {
            callback = function (err, resp, body) {
                let needsFallback = false;
                if (!err && resp && body) {
                    if (isJSONDoH === 'ali') {
                        const strBody = typeof body === 'string' ? body : (typeof Buffer !== 'undefined' ? Buffer.from(body).toString() : String.fromCharCode.apply(null, body));
                        try {
                            const arr = JSON.parse(strBody);
                            if (!Array.isArray(arr) || arr.length === 0) needsFallback = true;
                        } catch (e) { }
                    } else if (isJSONDoH === 'tencent') {
                        const strBody = typeof body === 'string' ? body : (typeof Buffer !== 'undefined' ? Buffer.from(body).toString() : String.fromCharCode.apply(null, body));
                        const arr = strBody.split(';').map(i => i.split(',')[0]).filter(i => i && i !== '0');
                        if (arr.length === 0) needsFallback = true;
                    } else if (isDoH && isAAAA) {
                        const u8 = (body instanceof Uint8Array) ? body : ((typeof Buffer !== 'undefined' && Buffer.isBuffer(body)) ? new Uint8Array(body.buffer, body.byteOffset, body.byteLength) : null);
                        if (u8 && u8.length >= 12) {
                            const ancount = (u8[6] << 8) | u8[7];
                            if (ancount === 0) needsFallback = true;
                        }
                    }
                }

                if (needsFallback && originalCallback) {
                    __enableDohFilterCompat();
                    const newReq = Object.assign({}, req);
                    let fallbackRawBody = null;
                    if (isJSONDoH) {
                        newReq.url = req.url.replace('type=AAAA', 'type=A');
                    } else if (isDoH && isAAAA && queryBuf) {
                        const newQueryBuf = new Uint8Array(queryBuf.length);
                        newQueryBuf.set(queryBuf);
                        let offset = 12;
                        while (offset < newQueryBuf.length) {
                            const len = newQueryBuf[offset];
                            if (len === 0) { offset++; break; }
                            if ((len & 0xC0) === 0xC0) { offset += 2; break; }
                            offset += len + 1;
                        }
                        if (offset + 4 <= newQueryBuf.length) {
                            newQueryBuf[offset] = 0;
                            newQueryBuf[offset + 1] = 1;
                        }

                        if (dohMethod === 'GET') {
                            const b64 = __uint8array_to_base64(newQueryBuf);
                            const b64url = b64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
                            newReq.url = req.url.replace(/([\?&]dns=)[^&]+/, '$1' + b64url);
                        } else if (dohMethod === 'POST') {
                            fallbackRawBody = newQueryBuf;
                            delete newReq.body;
                            delete newReq["body-base64"];
                        }
                    }

                    const fallbackReqId = "req_" + (++__http_req_id);
                    __http_callbacks[fallbackReqId] = originalCallback;
                    if (fallbackRawBody) {
                        __go_http_request_async(dohMethod, JSON.stringify(newReq), fallbackReqId, fallbackRawBody);
                    } else {
                        __go_http_request_async(dohMethod, JSON.stringify(newReq), fallbackReqId);
                    }
                    return;
                }

                if (originalCallback) originalCallback(err, resp, body);
            };
        }

        if (callback) { __http_callbacks[reqId] = callback; }
        if (rawBody) {
            __go_http_request_async(dohMethod, JSON.stringify(req), reqId, rawBody);
        } else {
            __go_http_request_async(dohMethod, JSON.stringify(req), reqId);
        }
    };
});

function __dispatch_http_response(reqId, resMetaJson, body) {
    const cb = __http_callbacks[reqId];
    if (!cb) return;
    delete __http_callbacks[reqId];

    const res = JSON.parse(resMetaJson);
    const err = res.error !== undefined && res.error !== null ? res.error : null;
    const resp = res.response !== undefined ? res.response : null;

    if (resp) {
        if (res.isBinary) {
            const u8 = body instanceof Uint8Array ? body : new Uint8Array(0);
            resp.body = typeof Buffer !== 'undefined'
                ? Buffer.from(u8.buffer, u8.byteOffset, u8.byteLength)
                : u8;
        } else {
            resp.body = body;
        }
    }
    cb(err, resp, resp ? resp.body : null);
}

// 原生的 UDP / DoH3 / DoQ / DoT DNS 解析能力
const $dns = {
    query: function (options, callback) {
        const reqId = "dns_" + (++__http_req_id);
        if (callback) {
            __http_callbacks[reqId] = callback;
        }
        __go_dns_query_async(JSON.stringify(options), reqId);
    }
};

function __dispatch_dns_response(reqId, resJson) {
    const cb = __http_callbacks[reqId];
    if (!cb) return;
    delete __http_callbacks[reqId];
    const res = JSON.parse(resJson);
    const err = res.error !== undefined && res.error !== null ? { message: res.error } : null;
    const resp = res.response !== undefined ? res.response : null;
    cb(err, resp);
}

// 定时器预编译进字节码，避免每次 Execute 重新编译入口脚本
globalThis.__timer_id = 0;
globalThis.__timers = {};
globalThis.setTimeout = function (fn, delay) {
    const id = String(++globalThis.__timer_id);
    globalThis.__timers[id] = fn;
    __go_set_timeout(id, Number(delay) || 0);
    return id;
};
globalThis.clearTimeout = function (id) {
    if (id !== undefined && id !== null && globalThis.__timers[id]) {
        delete globalThis.__timers[id];
        if (typeof __go_clear_timeout === 'function') {
            __go_clear_timeout(String(id));
        }
    }
};
globalThis.setInterval = function (fn, delay) {
    return setTimeout(fn, delay);
};
globalThis.clearInterval = globalThis.clearTimeout;

globalThis.__fire_timer = function (id) {
    const fn = globalThis.__timers[id];
    if (fn) {
        delete globalThis.__timers[id];
        try { fn(); } catch (e) { __console_log("error", "Timer Exception: " + String(e)); }
    }
};

// Go 原生 YAML / Clash Preprocessor / JSON5 快路径包装
globalThis.__wrap_yaml = function (orig) {
    if (!orig) return orig;
    const canGoParse = (s, opts) => {
        if (typeof s !== 'string') return false;
        if (!opts) return true;
        if (typeof opts === 'function') return false;
        if (opts.intAsBigInt || opts.mapAsMap || opts.keepSourceTokens || opts.customTags || opts.schema) return false;
        return true;
    };
    const canGoDump = (obj, opts) => {
        if (obj === undefined || typeof obj === 'function') return false;
        if (!opts) return true;
        if (typeof opts !== 'object') return false;
        if (opts.replacer || opts.sortKeys || opts.forceQuotes || opts.quotingType || opts.noArrayIndent || opts.noRefs === false) return false;
        return true;
    };

    const fastLoad = function (s, opts) {
        if (typeof __go_yaml_to_json === 'function' && canGoParse(s, opts)) {
            const jsonStr = __go_yaml_to_json(s);
            if (jsonStr && jsonStr.length > 0) {
                try { return JSON.parse(jsonStr); } catch (_) { }
            }
        }
        return orig.safeLoad(s, opts);
    };
    const fastDump = function (obj, opts) {
        if (typeof __go_json_to_yaml === 'function' && canGoDump(obj, opts)) {
            try {
                const yamlStr = __go_json_to_yaml(JSON.stringify(obj));
                if (yamlStr && yamlStr.length > 0) return yamlStr;
            } catch (_) { }
        }
        return orig.safeDump(obj, opts);
    };

    return {
        safeLoad: fastLoad,
        load: fastLoad,
        parse: fastLoad,
        safeDump: fastDump,
        dump: fastDump,
        stringify: fastDump
    };
};

globalThis.__wrap_clash_pre = function (orig) {
    let lastRaw = null;
    let lastRes = null;
    return {
        name: orig.name,
        test: function (raw) {
            if (typeof raw !== 'string' || raw.indexOf('proxies') === -1) {
                lastRaw = null;
                lastRes = null;
                return false;
            }
            if (typeof __go_clash_extract_proxies === 'function') {
                const jsonStr = __go_clash_extract_proxies(raw);
                if (jsonStr && jsonStr.length > 0) {
                    const parsed = JSON.parse(jsonStr);
                    if (!parsed.valid) {
                        lastRaw = null;
                        lastRes = null;
                        return false;
                    }
                    lastRaw = raw;
                    lastRes = parsed.proxies || [];
                    return true;
                }
            }
            lastRaw = null;
            lastRes = null;
            return orig.test(raw);
        },
        parse: function (raw, includeProxies) {
            let proxies = null;
            if (lastRaw === raw && Array.isArray(lastRes)) {
                proxies = lastRes;
                lastRaw = null;
                lastRes = null;
                const prefix = includeProxies ? 'proxies:\n' : '';
                const linePrefix = includeProxies ? '  - ' : '';
                const lines = new Array(proxies.length);
                for (let i = 0; i < proxies.length; i++) {
                    lines[i] = linePrefix + JSON.stringify(proxies[i]) + '\n';
                }
                return prefix + lines.join('');
            }
            lastRaw = null;
            lastRes = null;
            if (typeof __go_clash_extract_proxies === 'function') {
                const jsonStr = __go_clash_extract_proxies(raw);
                if (jsonStr && jsonStr.length > 0) {
                    const parsed = JSON.parse(jsonStr);
                    proxies = Array.isArray(parsed.proxies) ? parsed.proxies : [];
                    const prefix = includeProxies ? 'proxies:\n' : '';
                    const linePrefix = includeProxies ? '  - ' : '';
                    const lines = new Array(proxies.length);
                    for (let i = 0; i < proxies.length; i++) {
                        lines[i] = linePrefix + JSON.stringify(proxies[i]) + '\n';
                    }
                    return prefix + lines.join('');
                }
            }
            return orig.parse(raw, includeProxies);
        }
    };
};

globalThis.__wrap_json5 = function (orig) {
    let lastStr = null;
    let lastObj = null;
    return {
        parse: function (str, reviver) {
            if (!reviver && typeof str === 'string') {
                if (str === lastStr) return lastObj;
                if (str.charCodeAt(0) === 123) {
                    try {
                        const obj = JSON.parse(str);
                        lastStr = str;
                        lastObj = obj;
                        return obj;
                    } catch (_) { }
                }
            }
            const res = orig.parse(str, reviver);
            if (!reviver && typeof str === 'string') {
                lastStr = str;
                lastObj = res;
            }
            return res;
        },
        stringify: orig.stringify
    };
};

globalThis.__go_needs_short_id_norm = function (raw) {
    if (/&\S+\s+['"]?short-id['"]?/.test(raw)) return true;
    for (let idx = raw.indexOf('short-id'); idx !== -1; idx = raw.indexOf('short-id', idx + 8)) {
        let i = idx + 8;
        if (raw[i] === '"' || raw[i] === "'") i++;
        while (i < raw.length && (raw[i] === ' ' || raw[i] === '\t')) i++;
        if (raw[i] !== ':') continue;
        i++;
        while (i < raw.length && (raw[i] === ' ' || raw[i] === '\t')) i++;
        if (i >= raw.length || raw[i] === '\r' || raw[i] === '\n') return true;
        const quote = raw[i];
        if (quote === '"' || quote === "'") {
            let j = i + 1, closed = false;
            while (j < raw.length && raw[j] !== '\r' && raw[j] !== '\n') {
                if (quote === '"' && raw[j] === '\\') { j += 2; continue; }
                if (quote === "'" && raw[j] === "'" && raw[j + 1] === "'") { j += 2; continue; }
                if (raw[j] === quote) { closed = true; j++; break; }
                j++;
            }
            if (!closed) return true;
            while (j < raw.length && (raw[j] === ' ' || raw[j] === '\t')) j++;
            if (j < raw.length && raw[j] !== ',' && raw[j] !== '}' && raw[j] !== '\r' && raw[j] !== '\n' && raw[j] !== '#') return true;
            continue;
        }
        let j = i;
        while (j < raw.length) {
            const ch = raw[j];
            if (ch === ',' || ch === '}' || ch === '\r' || ch === '\n' || ch === '#') break;
            j++;
        }
        const val = raw.slice(i, j).trim();
        if (!val || !/^[0-9a-fA-F]+$/.test(val) || /^[0-9]+$/.test(val) || /^0[xXbBoO]/.test(val) || /^[0-9]+[eE][0-9]+$/.test(val)) {
            return true;
        }
    }
    return false;
};

// 在 JS 侧剥离巨型 Body 绕过 JSON 序列化
const $done = function (val) {
    let bodyStr = undefined;
    if (val && typeof val === 'object') {
        if (val.response && val.response.body !== undefined) {
            bodyStr = typeof val.response.body === 'string' ? val.response.body : JSON.stringify(val.response.body);
            delete val.response.body;
        } else if (val.body !== undefined) {
            bodyStr = typeof val.body === 'string' ? val.body : JSON.stringify(val.body);
            delete val.body;
        }
    }
    const metaJson = val ? JSON.stringify(val) : "{}";
    __done(metaJson, bodyStr || "");
    if (globalThis.__resolve_done) {
        const resolveFn = globalThis.__resolve_done;
        globalThis.__resolve_done = null;
        globalThis.__reject_done = null;
        resolveFn();
    }
};

function __reload_substore_caches(reloadMain, reloadResources) {
    if (reloadMain && globalThis.__substore_R && typeof globalThis.__substore_R.initCache === 'function') {
        globalThis.__substore_R.initCache();
    }
    if (reloadResources) {
        if (globalThis.__substore_dlCache && typeof globalThis.__substore_dlCache.clear === 'function') {
            globalThis.__substore_dlCache.clear();
        }
        const caches = globalThis.__substore_rcaches;
        if (Array.isArray(caches)) {
            const keys = [
                'sub-store-cached-resource',
                'sub-store-cached-headers-resource',
                'sub-store-cached-script-resource'
            ];
            for (let i = 0; i < caches.length; i++) {
                const c = caches[i];
                const k = keys[i];
                if (c && k) {
                    try {
                        const raw = $persistentStore.read(k);
                        c.resourceCache = raw ? JSON.parse(raw) : {};
                    } catch (_) {
                        c.resourceCache = {};
                    }
                }
            }
        }
    }
}

function __dispatch_sub_store(reqJson, argStr, reloadMain, reloadResources) {
    return new Promise((resolve, reject) => {
        for (const k in __http_callbacks) delete __http_callbacks[k];
        __http_req_id = 0;
        __log_cache.clear();
        if (__doh_fallback_used) {
            Array.prototype.filter = _originalFilter;
            __doh_fallback_used = false;
        }
        for (const id in globalThis.__timers) {
            clearTimeout(id);
        }

        const timeoutId = setTimeout(() => {
            reject(new Error("Sub-Store script timeout inside QuickJS"));
        }, 179000);
        globalThis.__resolve_done = function () {
            clearTimeout(timeoutId);
            resolve();
        };
        globalThis.__reject_done = function (reason) {
            clearTimeout(timeoutId);
            reject(new Error(reason || "Sub-Store script aborted"));
        };
        try {
            $request = JSON.parse(reqJson);
            if (!$request.headers || typeof $request.headers !== 'object') $request.headers = {};
            $argument = argStr;
            if (typeof globalThis.__substore_dispatch === 'function') {
                __reload_substore_caches(reloadMain, reloadResources);
                globalThis.__substore_dispatch($request);
            } else {
                __run_sub_store_script();
            }
        } catch (e) {
            clearTimeout(timeoutId);
            reject(e);
        }
    });
}

function __start_sub_store(reqJson, argStr) {
    return __dispatch_sub_store(reqJson, argStr, false, false);
}

function __abort_done(reason) {
    if (globalThis.__reject_done) {
        const rejectFn = globalThis.__reject_done;
        globalThis.__resolve_done = null;
        globalThis.__reject_done = null;
        rejectFn(reason);
    }
}

// 隔离执行主脚本
function __run_sub_store_script() {
    __SCRIPT_SRC__
}
