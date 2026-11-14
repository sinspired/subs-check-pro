// sub_store\init.js

// Web API Polyfill
(function (g) {
    if (!g.TextEncoder) {
        g.TextEncoder = class TextEncoder {
            encode(str) {
                // 尝试调用 Go 层编码器
                if (typeof __go_string_to_base64 === 'function') {
                    let b64 = __go_string_to_base64(str);
                    return __base64_to_uint8array(b64);
                }
                let arr = [];
                for (let i = 0; i < str.length; i++) {
                    let code = str.charCodeAt(i);
                    if (code < 0x80) arr.push(code);
                    else if (code < 0x800) arr.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f));
                    else if (code < 0xd800 || code >= 0xe000) arr.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f));
                    else {
                        i++; code = 0x10000 + (((code & 0x3ff) << 10) | (str.charCodeAt(i) & 0x3ff));
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
                // 如果有原始的 base64 字符串记录，直接抛给 Go 层 CGO 解码
                if (arr.__orig_b64 && typeof __go_base64_to_string === 'function') {
                    return __go_base64_to_string(arr.__orig_b64);
                }
                let str = "";
                for (let i = 0; i < arr.length; i++) {
                    let c = arr[i];
                    if (c < 0x80) str += String.fromCharCode(c);
                    else if (c > 0xbf && c < 0xe0) { str += String.fromCharCode(((c & 0x1f) << 6) | (arr[i + 1] & 0x3f)); i++; }
                    else if (c > 0xdf && c < 0xf0) { str += String.fromCharCode(((c & 0x0f) << 12) | ((arr[i + 1] & 0x3f) << 6) | (arr[i + 2] & 0x3f)); i += 2; }
                    else {
                        let code = (((c & 0x07) << 18) | ((arr[i + 1] & 0x3f) << 12) | ((arr[i + 2] & 0x3f) << 6) | (arr[i + 3] & 0x3f)) - 0x10000;
                        str += String.fromCharCode(0xd800 | (code >> 10), 0xdc00 | (code & 0x3ff)); i += 3;
                    }
                }
                return str;
            }
        };
    }
    if (!g.crypto) g.crypto = {};
    if (!g.crypto.getRandomValues) {
        g.crypto.getRandomValues = function (arr) {
            if (typeof __go_crypto_get_random_values === 'function') {
                let b64 = __go_crypto_get_random_values(arr.length);
                let _b64chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
                let _b64tab = new Uint8Array(256);
                for (let i = 0; i < _b64chars.length; i++) _b64tab[_b64chars.charCodeAt(i)] = i;

                b64 = b64.replace(/[^A-Za-z0-9\+\/\=]/g, '');
                let bufLen = (b64.length * 3) / 4;
                if (b64.charAt(b64.length - 1) === '=') bufLen--;
                if (b64.length > 1 && b64.charAt(b64.length - 2) === '=') bufLen--;

                let u8 = new Uint8Array(bufLen);
                let i = 0, j = 0;
                while (i < b64.length) {
                    let c1 = _b64tab[b64.charCodeAt(i++)];
                    let c2 = _b64tab[b64.charCodeAt(i++)];
                    let c3 = _b64tab[b64.charCodeAt(i++)];
                    let c4 = _b64tab[b64.charCodeAt(i++)];
                    let p = (c1 << 18) | (c2 << 12) | ((c3 !== undefined ? c3 : 0) << 6) | (c4 !== undefined ? c4 : 0);
                    u8[j++] = (p >> 16) & 255;
                    if (j < bufLen) u8[j++] = (p >> 8) & 255;
                    if (j < bufLen) u8[j++] = p & 255;
                }
                arr.set(u8);
            } else {
                for (let i = 0; i < arr.length; i++) arr[i] = Math.floor(Math.random() * 256);
            }
            return arr;
        };
    }
    if (!g.ReadableStream) g.ReadableStream = class ReadableStream { };
    if (!g.TransformStream) g.TransformStream = class TransformStream { };
    if (!g.Response) g.Response = class Response { };
})(globalThis);

// 加速排序期间的正则重复匹配（解决内置 RegexSortOperator 在无 JIT 引擎下重复跑几十万次正则的问题）
// 加速 RegexSortOperator：在 Array.prototype.sort 执行期间缓存 RegExp.prototype.test / String.prototype.search 结果
(function () {
    const _origSort = Array.prototype.sort;
    const _origTest = RegExp.prototype.test;
    const _origSearch = String.prototype.search;

    let _sortDepth = 0;
    let _regexTestCache = null;   // Map<RegExp, Map<string, boolean>>
    let _regexSearchCache = null; // Map<RegExp, Map<string, number>>

    RegExp.prototype.test = function (str) {
        if (_regexTestCache === null) {
            return _origTest.call(this, str);
        }
        let m = _regexTestCache.get(this);
        if (m === undefined) {
            if (!(this instanceof RegExp) || this.global || this.sticky) {
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
    };

    String.prototype.search = function (regexp) {
        if (_regexSearchCache === null) {
            return _origSearch.call(this, regexp);
        }
        let m = _regexSearchCache.get(regexp);
        if (m === undefined) {
            if (!(regexp instanceof RegExp) || regexp.global || regexp.sticky) {
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
    };

    Array.prototype.sort = function (compareFn) {
        if (typeof compareFn !== 'function' || this.length < 20) {
            return _origSort.call(this, compareFn);
        }
        const isOuter = _sortDepth === 0;
        _sortDepth++;
        if (isOuter) {
            _regexTestCache = new Map();
            _regexSearchCache = new Map();
        }
        try {
            return _origSort.call(this, compareFn);
        } finally {
            _sortDepth--;
            if (isOuter) {
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
        let val = __ps_read(key);
        return val === undefined ? null : val;
    },
    write: function (val, key) {
        __ps_write(val, key);
        return true;
    }
};

const $notification = {
    post: function (title, subtitle, content) {
        __notify_post(title, subtitle, content);
    }
};

// 智能日志防洪堤，阻断重复 Error/Warn 跨越 CGO 疯狂损耗性能
const __log_cache = {};
function __throttled_log(level, ...args) {
    let msg = args.map(String).join(" ");
    let key = level + ":" + msg;
    __log_cache[key] = (__log_cache[key] || 0) + 1;

    if (__log_cache[key] <= 3) {
        __console_log(level, msg);
    } else if (__log_cache[key] === 4) {
        __console_log(level, msg + " (⚠️ 该日志重复出现，已折叠隐藏...)");
    }
}

const console = {
    log: function (...args) { __throttled_log("log", ...args); },
    info: function (...args) { __throttled_log("info", ...args); },
    warn: function (...args) { __throttled_log("warn", ...args); },
    error: function (...args) { __throttled_log("error", ...args); }
};

// 拦截严格过滤，使 Sub-Store 平滑接受降级的 IPv4 记录
const _originalFilter = Array.prototype.filter;
Array.prototype.filter = function (fn) {
    if (this.length > 0) {
        let first = this[0];
        // 探测是不是 DNS answers 数组 { name, type, data/value, ... }
        if (first && typeof first === 'object' && ('type' in first) && (('data' in first) || ('value' in first))) {
            let fnStr = fn.toString();
            // 命中 ResolveDomainOperator 中的 `i?.type === answerType` 严格过滤
            if (fnStr.indexOf('.type') !== -1 && fnStr.indexOf('===') !== -1) {
                let strictRes = _originalFilter.call(this, fn);
                if (strictRes.length > 0) return strictRes; // 如果有 AAAA 记录，优先使用 AAAA
                // 如果严格匹配不到 (说明只有 A 记录)，放行 A 记录进行向下兼容
                return _originalFilter.call(this, function (i) {
                    return i?.type === 'A' || i?.type === 1 || i?.type === 'AAAA' || i?.type === 28;
                });
            }
        }
    }
    return _originalFilter.call(this, fn);
};

// JS 侧的无损二进制/Base64转换器
const _b64chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
const _b64tab = new Uint8Array(256);
for (let i = 0; i < _b64chars.length; i++) _b64tab[_b64chars.charCodeAt(i)] = i;

function __base64_to_uint8array(b64) {
    // 正则必须保留 '='，确保 padding 长度计算不发生 1~2 个字节的残缺丢包！
    b64 = b64.replace(/[^A-Za-z0-9\+\/\=]/g, '');
    let len = b64.length;
    if (len === 0) return new Uint8Array(0);

    let bufLen = (len * 3) / 4;
    if (b64.charAt(len - 1) === '=') bufLen--;
    if (len > 1 && b64.charAt(len - 2) === '=') bufLen--;

    let u8 = new Uint8Array(bufLen);
    let i = 0, j = 0;
    while (i < len) {
        let c1 = _b64tab[b64.charCodeAt(i++)];
        let c2 = _b64tab[b64.charCodeAt(i++)];
        let c3 = _b64tab[b64.charCodeAt(i++)];
        let c4 = _b64tab[b64.charCodeAt(i++)];

        let p = (c1 << 18) | (c2 << 12) | ((c3 !== undefined ? c3 : 0) << 6) | (c4 !== undefined ? c4 : 0);
        u8[j++] = (p >> 16) & 255;
        if (j < bufLen) u8[j++] = (p >> 8) & 255;
        if (j < bufLen) u8[j++] = p & 255;
    }

    // 强制转换为 Buffer，确保内部兼容 Node 原生库的包解析 API
    let res = typeof Buffer !== 'undefined' ? Buffer.from(u8.buffer, u8.byteOffset, u8.byteLength) : u8;
    // 保留特征值，供 TextDecoder 快速逃逸至原生解码
    res.__orig_b64 = b64;
    return res;
}

function __uint8array_to_base64(u8) {
    let b64 = "";
    let i = 0, len = u8.length;
    while (i < len) {
        let c1 = u8[i++];
        let c2 = i < len ? u8[i++] : NaN;
        let c3 = i < len ? u8[i++] : NaN;
        let p = (c1 << 16) | ((isNaN(c2) ? 0 : c2) << 8) | (isNaN(c3) ? 0 : c3);
        b64 += _b64chars.charAt((p >> 18) & 63);
        b64 += _b64chars.charAt((p >> 12) & 63);
        b64 += isNaN(c2) ? "=" : _b64chars.charAt((p >> 6) & 63);
        b64 += isNaN(c3) ? "=" : _b64chars.charAt(p & 63);
    }
    return b64;
}

const $httpClient = {};
const __http_callbacks = {};
let __http_req_id = 0;

['get', 'post', 'put', 'patch', 'delete', 'head', 'options'].forEach(method => {
    $httpClient[method] = function (options, callback) {
        let req = typeof options === 'string' ? { url: options } : Object.assign({}, options);

        if (req.body && (req.body instanceof Uint8Array || (typeof Buffer !== 'undefined' && Buffer.isBuffer(req.body)))) {
            req.body = __uint8array_to_base64(req.body);
            req["body-base64"] = true;
        }

        let reqId = "req_" + (++__http_req_id);

        // DoH 解析如果 AAAA 无结果，自动回退请求 A
        let originalCallback = callback;
        let isDoH = false, isAAAA = false, isJSONDoH = false;
        let dohMethod = method.toUpperCase();
        let queryBuf = null;

        // 探测 Ali / Tencent 的 JSON DoH 请求格式
        if (req.url && req.url.indexOf('223.6.6.6/resolve') !== -1 && req.url.indexOf('type=AAAA') !== -1) {
            isJSONDoH = 'ali';
        } else if (req.url && req.url.indexOf('119.28.28.28/d') !== -1 && req.url.indexOf('type=AAAA') !== -1) {
            isJSONDoH = 'tencent';
        } else {
            // 探测主流二进制 DoH (Google, Cloudflare, Custom DNS)
            let accept = req.headers && (req.headers['accept'] || req.headers['Accept']);
            let ctype = req.headers && (req.headers['content-type'] || req.headers['Content-Type']);
            if (accept === 'application/dns-message' || ctype === 'application/dns-message' || (typeof req.url === 'string' && req.url.indexOf('dns-query') !== -1)) {
                isDoH = true;
            }
        }

        if (isDoH) {
            // 解析出原始的二进制 DNS Query Buffer 判断是否是请求 IPv6 (QTYPE=28)
            if (dohMethod === 'GET') {
                let match = req.url.match(/[\?&]dns=([^&]+)/);
                if (match) {
                    let b64url = match[1];
                    let b64 = b64url.replace(/-/g, '+').replace(/_/g, '/');
                    while (b64.length % 4) b64 += '=';
                    queryBuf = __base64_to_uint8array(b64);
                }
            } else if (dohMethod === 'POST' && req.body) {
                if (req["body-base64"]) {
                    queryBuf = __base64_to_uint8array(req.body);
                } else if (typeof req.body === 'string') {
                    queryBuf = __base64_to_uint8array(req.body);
                }
            }
            if (queryBuf && queryBuf.length > 12) {
                let offset = 12;
                while (offset < queryBuf.length) {
                    let len = queryBuf[offset];
                    if (len === 0) { offset++; break; }
                    if ((len & 0xC0) === 0xC0) { offset += 2; break; }
                    offset += len + 1;
                }
                if (offset + 4 <= queryBuf.length) {
                    let qtype = (queryBuf[offset] << 8) | queryBuf[offset + 1];
                    if (qtype === 28) isAAAA = true;
                }
            }
        }

        // 接管回调并评估是否需要回退
        if ((isDoH && isAAAA) || isJSONDoH) {
            callback = function (err, resp, body) {
                let needsFallback = false;
                if (!err && resp && body) {
                    if (isJSONDoH === 'ali') {
                        let strBody = typeof body === 'string' ? body : (typeof Buffer !== 'undefined' ? Buffer.from(body).toString() : String.fromCharCode.apply(null, body));
                        try {
                            let arr = JSON.parse(strBody);
                            if (!Array.isArray(arr) || arr.length === 0) needsFallback = true;
                        } catch (e) { }
                    } else if (isJSONDoH === 'tencent') {
                        let strBody = typeof body === 'string' ? body : (typeof Buffer !== 'undefined' ? Buffer.from(body).toString() : String.fromCharCode.apply(null, body));
                        let arr = strBody.split(';').map(i => i.split(',')[0]).filter(i => i && i !== '0');
                        if (arr.length === 0) needsFallback = true;
                    } else if (isDoH && isAAAA) {
                        let u8 = (body instanceof Uint8Array) ? body : ((typeof Buffer !== 'undefined' && Buffer.isBuffer(body)) ? new Uint8Array(body) : null);
                        // 通过 DNS 包头验证 ANCOUNT(Answer) 字段是否为 0
                        if (u8 && u8.length >= 12) {
                            let ancount = (u8[6] << 8) | u8[7];
                            if (ancount === 0) needsFallback = true;
                        }
                    }
                }

                if (needsFallback && originalCallback) {
                    let newReq = Object.assign({}, req);
                    if (isJSONDoH) {
                        // JSON 请求改写参数为 IPv4 (A)
                        newReq.url = req.url.replace('type=AAAA', 'type=A');
                    } else if (isDoH && isAAAA && queryBuf) {
                        // 二进制请求改写 DNS Buffer 为 IPv4 (QTYPE 1)
                        let newQueryBuf = new Uint8Array(queryBuf.length);
                        newQueryBuf.set(queryBuf);
                        let offset = 12;
                        while (offset < newQueryBuf.length) {
                            let len = newQueryBuf[offset];
                            if (len === 0) { offset++; break; }
                            if ((len & 0xC0) === 0xC0) { offset += 2; break; }
                            offset += len + 1;
                        }
                        if (offset + 4 <= newQueryBuf.length) {
                            newQueryBuf[offset] = 0;
                            newQueryBuf[offset + 1] = 1; // 修改为查询 A 记录
                        }

                        if (dohMethod === 'GET') {
                            let b64 = __uint8array_to_base64(newQueryBuf);
                            let b64url = b64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
                            newReq.url = req.url.replace(/([\?&]dns=)[^&]+/, '$1' + b64url);
                        } else if (dohMethod === 'POST') {
                            newReq.body = __uint8array_to_base64(newQueryBuf);
                            newReq["body-base64"] = true;
                        }
                    }

                    // 无缝发起二次请求 (退避重试)
                    let fallbackReqId = "req_" + (++__http_req_id);
                    __http_callbacks[fallbackReqId] = originalCallback;
                    __go_http_request_async(method.toUpperCase(), JSON.stringify(newReq), fallbackReqId);
                    return;
                }

                if (originalCallback) originalCallback(err, resp, body);
            };
        }

        if (callback) { __http_callbacks[reqId] = callback; }
        __go_http_request_async(method.toUpperCase(), JSON.stringify(req), reqId);
    };
});

function __dispatch_http_response(reqId, resMetaJson) {
    let cb = __http_callbacks[reqId];
    if (!cb) return;
    delete __http_callbacks[reqId];

    let res = JSON.parse(resMetaJson);
    let err = res.error !== undefined && res.error !== null ? res.error : null;
    let resp = res.response !== undefined ? res.response : null;

    if (resp && res.reqId !== undefined) {
        let rawBody = __go_http_request_body(res.reqId);
        if (resp.headers && resp.headers["__is_binary__"] === "true") {
            delete resp.headers["__is_binary__"];
            resp.body = __base64_to_uint8array(rawBody);
        } else {
            resp.body = rawBody;
        }
    }
    cb(err, resp, resp ? resp.body : null);
}

// 原生的 UDP DNS 解析能力
const $dns = {
    query: function (options, callback) {
        let reqId = "dns_" + (++__http_req_id);
        if (callback) {
            __http_callbacks[reqId] = callback;
        }
        __go_dns_query_async(JSON.stringify(options), reqId);
    }
};

function __dispatch_dns_response(reqId, resJson) {
    let cb = __http_callbacks[reqId];
    if (!cb) return;
    delete __http_callbacks[reqId];
    let res = JSON.parse(resJson);
    let err = res.error !== undefined && res.error !== null ? { message: res.error } : null;
    let resp = res.response !== undefined ? res.response : null;
    cb(err, resp);
}

// 终极优化：在 JS 侧剥离巨型 Body 绕过 JSON 序列化
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
    let metaJson = val ? JSON.stringify(val) : "{}";
    __done(metaJson, bodyStr || "");
};

// 隔离执行主脚本
function __run_sub_store_script() {
    __SCRIPT_SRC__
}
