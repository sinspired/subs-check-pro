// analysis.js

// 国家中心坐标 [lon, lat]
const GEO_COUNTRY_COORDS = {
    // 东亚
    CN: [104.20, 35.86], HK: [114.17, 22.32], TW: [120.96, 23.70],
    JP: [138.25, 36.20], KR: [127.77, 35.91], MN: [103.85, 46.86], MO: [113.54, 22.20],
    KP: [127.51, 40.34],

    // 东南亚
    SG: [103.82, 1.35], TH: [100.99, 15.87], MY: [109.70, 4.21], ID: [113.92, -0.79],
    VN: [108.28, 14.06], PH: [122.88, 12.88], MM: [95.96, 16.87], KH: [104.99, 12.57],
    LA: [102.50, 17.97], BN: [114.73, 4.54], TL: [125.73, -8.87],

    // 南亚
    IN: [78.96, 20.59], BD: [90.36, 23.68], PK: [69.34, 30.38], LK: [80.77, 7.87],
    NP: [84.12, 28.39], AF: [67.71, 33.93], BT: [90.43, 27.51], MV: [73.22, 3.20],

    // 中亚
    KZ: [66.92, 48.02], UZ: [63.57, 41.38], TM: [58.99, 38.97],
    KG: [74.77, 41.20], TJ: [71.28, 38.86],

    // 高加索
    GE: [43.36, 42.32], AM: [45.04, 40.07], AZ: [47.58, 40.14],

    // 西亚/中东
    TR: [35.24, 38.96], IL: [34.85, 31.05], AE: [53.85, 23.42], SA: [45.08, 23.89],
    QA: [51.18, 25.35], KW: [47.48, 29.31], IQ: [43.68, 33.22], IR: [53.69, 32.43],
    JO: [36.24, 30.59], LB: [35.86, 33.85], OM: [57.55, 21.51], BH: [50.55, 26.07],
    YE: [48.52, 15.55], SY: [38.99, 34.80], PS: [35.30, 31.95],

    // 欧洲西部
    GB: [-3.44, 55.38], FR: [2.21, 46.23], DE: [10.45, 51.17], NL: [5.29, 52.13],
    BE: [4.47, 50.50], LU: [6.13, 49.82], CH: [8.23, 46.82], AT: [14.55, 47.52],
    IE: [-8.24, 53.41], PT: [-8.22, 39.40], ES: [-3.75, 40.46], IT: [12.57, 41.87],
    GR: [21.82, 39.07], MT: [14.40, 35.90], CY: [33.43, 35.13],
    MC: [7.41, 43.73], LI: [9.56, 47.17], AD: [1.52, 42.55],
    SM: [12.46, 43.94], VA: [12.45, 41.90],

    // 欧洲北部
    SE: [18.64, 60.13], NO: [8.47, 60.47], DK: [9.50, 56.26],
    FI: [25.75, 61.92], IS: [-18.14, 64.96],

    // 欧洲东部
    RU: [105.32, 61.52], UA: [31.17, 48.38], BY: [28.05, 53.71], MD: [28.37, 47.41],
    LT: [23.88, 55.17], LV: [24.60, 56.88], EE: [25.01, 58.60],
    PL: [19.15, 51.92], CZ: [15.47, 49.82], SK: [19.70, 48.67], HU: [19.50, 47.16],
    RO: [24.97, 45.94], BG: [25.49, 42.73], HR: [15.20, 45.10], RS: [21.01, 44.02],
    SI: [14.82, 46.15], BA: [17.68, 43.92], MK: [21.75, 41.61], AL: [20.17, 41.15],
    ME: [19.37, 42.71], XK: [20.90, 42.60],

    // 北美
    US: [-95.71, 37.09], CA: [-96.80, 60.00], MX: [-102.55, 23.63],

    // 中美洲 & 加勒比
    GT: [-90.23, 15.78], BZ: [-88.50, 17.19], HN: [-86.24, 15.20], SV: [-88.90, 13.79],
    NI: [-85.21, 12.87], CR: [-83.75, 9.75], PA: [-80.78, 8.54],
    CU: [-79.52, 21.52], JM: [-77.30, 18.11], HT: [-72.29, 18.97], DO: [-70.16, 18.74],
    PR: [-66.59, 18.22], TT: [-61.22, 10.69], BB: [-59.54, 13.19],

    // 南美
    CO: [-74.30, 4.57], VE: [-66.59, 6.42], GY: [-58.93, 4.86], SR: [-56.03, 3.92],
    BR: [-51.93, -14.24], EC: [-77.82, -1.83], PE: [-75.02, -9.19], BO: [-64.74, -17.34],
    PY: [-58.44, -23.44], UY: [-55.77, -32.52], AR: [-63.62, -38.42], CL: [-71.54, -35.68],

    // 非洲北部
    MA: [-7.09, 31.79], DZ: [1.66, 28.03], TN: [9.54, 33.89], LY: [17.23, 26.34],
    EG: [30.80, 26.82], SD: [29.91, 12.86], SS: [31.31, 7.86], MR: [-10.94, 20.26],

    // 非洲西部
    SN: [-14.45, 14.50], GM: [-15.31, 13.44], GW: [-15.18, 11.80], GN: [-11.35, 9.95],
    SL: [-11.78, 8.46], LR: [-  9.43, 6.43], CI: [-5.55, 7.54], GH: [-1.02, 7.95],
    TG: [0.82, 8.62], BJ: [2.32, 9.31], NG: [8.68, 9.08], BF: [-1.56, 12.36],
    ML: [-1.98, 17.57], NE: [8.08, 17.61], MR: [-10.94, 20.26],

    // 非洲中部
    CM: [12.35, 5.70], CF: [20.94, 6.61], TD: [18.73, 15.45], GQ: [8.08, 1.65],
    GA: [11.61, -0.80], CG: [15.83, -0.23], CD: [24.68, -2.88], AO: [17.87, -11.20],

    // 非洲东部
    ET: [40.49, 9.15], ER: [39.78, 15.18], DJ: [42.59, 11.83], SO: [45.34, 6.11],
    KE: [37.91, -0.02], UG: [32.29, 1.37], RW: [29.87, -1.94], BI: [29.92, -3.38],
    TZ: [34.89, -6.37], MZ: [35.53, -18.67], MW: [34.30, -13.25], ZM: [27.85, -13.13],
    ZW: [29.15, -19.02], MG: [46.87, -18.77],

    // 非洲南部
    ZA: [25.08, -29.00], NA: [18.49, -22.96], BW: [24.68, -22.33],
    LS: [28.23, -29.61], SZ: [31.47, -26.52],

    // 大洋洲
    AU: [133.78, -25.27], NZ: [174.89, -40.90], PG: [143.96, -6.31], FJ: [179.41, -16.58],
    SB: [160.16, -9.65], VU: [166.96, -15.38], WS: [-172.10, -13.76], TO: [-175.20, -21.18],
    KI: [173.02, 1.33], FM: [150.55, 6.92], PW: [134.58, 7.51],
};

function hexToRgba(hex, a) {
    const r = parseInt(hex.slice(1, 3), 16);
    const g = parseInt(hex.slice(3, 5), 16);
    const b = parseInt(hex.slice(5, 7), 16);
    return `rgba(${r},${g},${b},${a})`;
}

function loadScript(src) {
    return new Promise((res, rej) => {
        if (document.querySelector(`script[src="${src}"]`)) return res();
        const s = document.createElement('script');
        s.src = src; s.onload = res; s.onerror = rej;
        document.head.appendChild(s);
    });
}

class GeoFlightMap {
    constructor(container, entries, origin, visibilityFn = null) {
        this._visibilityFn = visibilityFn;   // ← 新增
        this.container = container;
        this.origin = origin;
        this.entries = entries.filter(([c]) => GEO_COUNTRY_COORDS[c]);
        this.dpr = Math.min(window.devicePixelRatio || 1, 2);
        this.maxCount = this.entries[0]?.[1] || 1;
        this.raf = null;
        this.t0 = null;
        this.W = this.H = 0;

        this.canvas = document.createElement('canvas');
        this.canvas.style.cssText = 'display:block;width:100%;height:100%;';
        this.ctx = this.canvas.getContext('2d');
        container.innerHTML = '';
        container.appendChild(this.canvas);

        this._addLegend(container);
        this._addOriginHint(container);

        this.resize();
        this.buildArcs();
        this._worldReady = false;
        this._loadWorld().then(() => { this.t0 = null; });
        this._startAnim();

        this._ro = new ResizeObserver(() => { this.resize(); this.buildArcs(); this.t0 = null; });
        this._ro.observe(container);

        // hover 检测
        this._hoveredCode = null;
        this._onMouseMove = (e) => {
            const rect = this.canvas.getBoundingClientRect();
            const mx = (e.clientX - rect.left) * (this.W / rect.width);
            const my = (e.clientY - rect.top) * (this.H / rect.height);
            let found = null;
            for (const arc of this.arcs) {
                const dist = Math.hypot(mx - arc.x2, my - arc.y2);
                if (dist < 16 * (this.scale || 1)) { found = arc.code; break; }
            }
            if (found !== this._hoveredCode) {
                this._hoveredCode = found;
                this.canvas.style.cursor = found ? 'pointer' : '';
            }
        };
        this._onMouseLeave = () => { this._hoveredCode = null; this.canvas.style.cursor = ''; };
        this.canvas.addEventListener('mousemove', this._onMouseMove);
        this.canvas.addEventListener('mouseleave', this._onMouseLeave);
    }

    // 图例 & 提示 overlay
    _addLegend(container) {
        const visibleRegions = new Set(this.entries.map(([c]) => GEO_REGIONS[c] || '其他'));
        const html = [...visibleRegions].map(r =>
            `<div class="map-overlay-item"><span class="map-overlay-dot" style="background:${REGION_COLORS[r] || '#64748b'}"></span>${r}</div>`
        ).join('');
        const div = document.createElement('div');
        div.className = 'map-overlay-legend';
        div.innerHTML = html;
        container.appendChild(div);
    }

    _addOriginHint(container) {
        const div = document.createElement('div');
        div.className = 'map-origin-hint';
        div.id = 'mapOriginHint';
        div.textContent = '● 当前位置';
        container.appendChild(div);
    }

    resize() {
        const W = this.container.clientWidth || 640;
        const H = this.container.clientHeight || 240;
        this.W = W; this.H = H;
        this.canvas.width = W * this.dpr;
        this.canvas.height = H * this.dpr;
        this.canvas.style.width = W + 'px';
        this.canvas.style.height = H + 'px';
        this.ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
        this._initScale();
    }

    _initScale() {
        // 以 900×260 为基准尺寸，等比缩放所有视觉元素
        const base = Math.sqrt(900 * 260);
        const cur = Math.sqrt(this.W * this.H);
        this.scale = Math.max(0.45, Math.min(1.8, cur / base));
        // 移动端降低 DPR 渲染压力
        this.isMobile = this.W < 520;
    }

    proj(lon, lat) {
        const mx = 2, my = 4;
        const centerLon = this.origin.lon + 35; // 中心东移 35°，China 偏左，美洲入画
        let dlon = lon - centerLon;
        while (dlon > 180) dlon -= 360;
        while (dlon < -180) dlon += 360;
        // 原点居中，±180° 精确到画布两侧
        const x = mx + (dlon + 180) / 360 * (this.W - 2 * mx);
        // 纬度范围 85°N ~ 60°S（145° 总跨度，裁掉极地空洞）
        const y = my + (85 - lat) / 145 * (this.H - 2 * my);
        return [x, y];
    }

    _projRaw(dlon, lat) {
        const mx = 2, my = 4;
        const x = mx + (dlon + 180) / 360 * (this.W - 2 * mx);
        const y = my + (85 - lat) / 145 * (this.H - 2 * my);
        return [x, y];
    }

    // 建弧线数据
    buildArcs() {
        const [ox, oy] = this.proj(this.origin.lon, this.origin.lat);
        this.ox = ox; this.oy = oy;
        const sc = this.scale || 1;

        this.arcs = this.entries.map(([code, count], i) => {
            const [lon, lat] = GEO_COUNTRY_COORDS[code];
            const [dx, dy] = this.proj(lon, lat);
            const region = GEO_REGIONS[code] || '其他';
            const color = REGION_COLORS[region] || '#64748b';
            const ratio = count / this.maxCount;
            const dist = Math.hypot(dx - ox, dy - oy);
            const lift = Math.min(dist * 0.42, this.H * 0.45);
            const lineW = (0.5 + ratio * 1.8) * sc;
            const planeSize = (3 + ratio * 3) * sc;
            const speedFactor = this.isMobile ? 1.25 : 1;
            return {
                code, count, ratio, color,
                x1: ox, y1: oy,
                cpx: (ox + dx) / 2, cpy: (oy + dy) / 2 - lift,
                x2: dx, y2: dy,
                lineW, planeSize,
                delay: i * (this.isMobile ? 90 : 60),
                drawDur: (700 + (1 - ratio) * 400) * speedFactor,
                period: (3000 + i * 120) * speedFactor,
            };
        });
    }

    bzPt(t, x1, y1, cpx, cpy, x2, y2) {
        const m = 1 - t;
        return [m * m * x1 + 2 * m * t * cpx + t * t * x2, m * m * y1 + 2 * m * t * cpy + t * t * y2];
    }

    bzAngle(t, x1, y1, cpx, cpy, x2, y2) {
        const m = 1 - t;
        return Math.atan2(2 * m * (cpy - y1) + 2 * t * (y2 - cpy), 2 * m * (cpx - x1) + 2 * t * (x2 - cpx));
    }

    // 异步加载真实地图数据
    async _loadWorld() {
        if (GeoFlightMap._worldGeo) { this._worldReady = true; return; }
        if (GeoFlightMap._worldLoading) {
            await new Promise(res => {
                const t = setInterval(() => {
                    if (GeoFlightMap._worldGeo || !GeoFlightMap._worldLoading) { clearInterval(t); res(); }
                }, 100);
            });
            this._worldReady = !!GeoFlightMap._worldGeo;
            return;
        }
        GeoFlightMap._worldLoading = true;
        try {
            await loadScript('https://cdn.jsdelivr.net/npm/topojson-client@3/dist/topojson-client.min.js');
            const resp = await fetch('https://cdn.jsdelivr.net/npm/world-atlas@2/countries-110m.json');
            const topo = await resp.json();
            GeoFlightMap._worldGeo = window.topojson.feature(topo, topo.objects.land);
            GeoFlightMap._worldLoading = false;
            this._worldReady = true;
        } catch (e) {
            console.warn('[GeoFlightMap] 地图数据加载失败，回退点阵模式', e);
            GeoFlightMap._worldLoading = false;
            this._worldReady = false;
        }
    }

    drawWorldMap() {
        if (!this._worldReady || !GeoFlightMap._worldGeo) { this._drawDotFallback(); return; }
        const { ctx } = this;
        ctx.save();
        ctx.beginPath();
        ctx.rect(0, 0, this.W, this.H);
        ctx.clip();
        const geo = GeoFlightMap._worldGeo;
        const features = geo.type === 'FeatureCollection' ? geo.features
            : geo.type === 'Feature' ? [geo]
                : [{ geometry: geo }];
        for (const f of features) { if (f.geometry) this._drawGeometry(f.geometry); }
        ctx.restore();
    }

    _drawGeometry(geometry) {
        if (!geometry) return;
        if (geometry.type === 'Polygon') {
            this._drawRings(geometry.coordinates);
        } else if (geometry.type === 'MultiPolygon') {
            for (const poly of geometry.coordinates) this._drawRings(poly);
        } else if (geometry.type === 'GeometryCollection') {
            for (const g of geometry.geometries) this._drawGeometry(g);
        }
    }

    _drawRings(rings) {
        const { ctx } = this;
        ctx.beginPath();
        for (const ring of rings) {
            if (ring.length < 3) continue;
            let prevLonRaw = null, curDlon = 0;
            for (let i = 0; i < ring.length; i++) {
                const [lon, lat] = ring[i];
                if (i === 0) {
                    curDlon = lon - (this.origin.lon + 35);
                    while (curDlon > 180) curDlon -= 360;
                    while (curDlon < -180) curDlon += 360;
                } else {
                    let step = lon - prevLonRaw;
                    while (step > 180) step -= 360;
                    while (step < -180) step += 360;
                    curDlon += step;
                }
                prevLonRaw = lon;
                const [px, py] = this._projRaw(curDlon, lat);
                i === 0 ? ctx.moveTo(px, py) : ctx.lineTo(px, py);
            }
            ctx.closePath();
        }
        ctx.fillStyle = 'rgba(18, 52, 100, 0.60)';
        ctx.fill('evenodd');
        ctx.strokeStyle = 'rgba(70, 140, 220, 0.38)';
        ctx.lineWidth = 0.5;
        ctx.lineJoin = 'round';
        ctx.stroke();
    }

    _drawDotFallback() {
        const { ctx } = this;
        const destSet = new Set(this.entries.map(([c]) => c));
        // ctx.fillStyle = 'rgba(255,255,255,0.08)';
        for (const [code, [lon, lat]] of Object.entries(GEO_COUNTRY_COORDS)) {
            let d = lon - this.origin.lon;
            while (d > 180) d -= 360;
            while (d < -180) d += 360;
            const [x, y] = this.proj(this.origin.lon + d, lat);
            ctx.beginPath();
            ctx.arc(x, y, destSet.has(code) ? 1.5 : 0.8, 0, Math.PI * 2);
            ctx.fillStyle = destSet.has(code) ? 'rgba(255,248,180,0.5)' : 'rgba(255,255,255,0.07)';
            ctx.fill();
        }
    }

    // 背景
    drawBg() {
        const { ctx, W, H } = this;
        const grad = ctx.createLinearGradient(0, 0, 0, H);
        grad.addColorStop(0, '#04091a');
        grad.addColorStop(1, '#070d22');
        ctx.fillStyle = grad;
        ctx.fillRect(0, 0, W, H);

        ctx.save();
        ctx.strokeStyle = 'rgba(255,255,255,0.035)';
        ctx.lineWidth = 0.5;
        for (let d = -180; d <= 180; d += 30) {
            const lon = this.origin.lon + 35 + d;
            const [x, y1] = this.proj(lon, 85);
            const [, y2] = this.proj(lon, -60);
            ctx.beginPath(); ctx.moveTo(x, y1); ctx.lineTo(x, y2); ctx.stroke();
        }
        for (let lat = -60; lat <= 85; lat += 30) {
            const [x1, y] = this.proj(this.origin.lon + 35 - 180, lat);
            const [x2] = this.proj(this.origin.lon + 35 + 180, lat);
            ctx.beginPath(); ctx.moveTo(x1, y); ctx.lineTo(x2, y); ctx.stroke();
        }
        ctx.restore();

        // 大陆轮廓
        this.drawWorldMap();


        // 城市灯光
        const destSet = new Set(this.entries.map(([c]) => c));
        for (const [code, [lon, lat]] of Object.entries(GEO_COUNTRY_COORDS)) {
            let d = lon - this.origin.lon;
            while (d > 180) d -= 360;
            while (d < -180) d += 360;
            const [x, y] = this.proj(this.origin.lon + d, lat);
            const isDest = destSet.has(code);
            ctx.beginPath();
            ctx.arc(x, y, isDest ? 1.6 : 0.7, 0, Math.PI * 2);
            ctx.fillStyle = isDest ? 'rgba(255,248,180,0.6)' : 'rgba(255,255,255,0.06)';
            ctx.fill();
        }
    }

    // 弧线
    drawArc(arc, progress) {
        if (progress <= 0) return;
        const { ctx } = this;
        const { x1, y1, cpx, cpy, x2, y2, color, ratio, lineW } = arc;
        const sc = this.scale || 1;
        const STEPS = this.isMobile ? 40 : 64;
        const end = Math.ceil(progress * STEPS);

        ctx.save();
        ctx.shadowColor = color;
        ctx.shadowBlur = (3 + ratio * 8) * sc;
        ctx.strokeStyle = hexToRgba(color, 0.18 + ratio * 0.52);
        ctx.lineWidth = lineW;
        ctx.lineCap = 'round';
        ctx.beginPath();
        for (let i = 0; i <= end; i++) {
            const s = (i / end) * progress;
            const mt = 1 - s;
            const px = mt * mt * x1 + 2 * mt * s * cpx + s * s * x2;
            const py = mt * mt * y1 + 2 * mt * s * cpy + s * s * y2;
            i === 0 ? ctx.moveTo(px, py) : ctx.lineTo(px, py);
        }
        ctx.stroke();
        // 外发光宽线
        ctx.shadowBlur = 0;
        ctx.strokeStyle = hexToRgba(color, 0.05 + ratio * 0.08);
        ctx.lineWidth = lineW * 3.5;
        ctx.stroke();
        ctx.restore();
    }

    // 飞机图标
    drawPlane(arc, progress, now) {
        if (progress < 1) return;
        const { ctx } = this;
        const { x1, y1, cpx, cpy, x2, y2, color, ratio, period, planeSize } = arc;
        const t = (now % period) / period;
        const [px, py] = this.bzPt(t, x1, y1, cpx, cpy, x2, y2);
        const angle = this.bzAngle(t, x1, y1, cpx, cpy, x2, y2);
        const sz = planeSize;

        ctx.save();
        ctx.translate(px, py);
        ctx.rotate(angle);
        ctx.shadowColor = color;
        ctx.shadowBlur = 8 * (this.scale || 1);
        ctx.globalAlpha = 0.85 + ratio * 0.15;

        // 机身
        ctx.fillStyle = '#ffffff';
        ctx.beginPath();
        ctx.moveTo(sz * 1.4, 0);
        ctx.lineTo(-sz * 0.8, -sz * 0.45);
        ctx.lineTo(-sz * 0.3, 0);
        ctx.lineTo(-sz * 0.8, sz * 0.45);
        ctx.closePath();
        ctx.fill();

        // 机翼彩色高光
        ctx.fillStyle = hexToRgba(color, 0.7);
        ctx.beginPath();
        ctx.moveTo(-sz * 0.1, -sz * 0.1);
        ctx.lineTo(-sz * 0.7, -sz * 0.9);
        ctx.lineTo(-sz * 1.0, -sz * 0.4);
        ctx.lineTo(-sz * 0.5, 0);
        ctx.closePath();
        ctx.fill();
        ctx.restore();
    }

    // 目的地光点
    drawDest(arc, progress, now) {
        if (progress < 0.75) return;
        const alpha = Math.min(1, (progress - 0.75) / 0.25);
        const { ctx } = this;
        const { x2, y2, color, ratio, code } = arc;
        const sc = this.scale || 1;
        const r = (2 + ratio * 4) * sc;
        const pulse = 0.4 + 0.6 * Math.sin(now * 0.003 + arc.x2 * 0.1);

        ctx.save();
        ctx.globalAlpha = alpha;
        ctx.beginPath();
        ctx.arc(x2, y2, r + (2 + pulse * 4) * sc, 0, Math.PI * 2);
        ctx.strokeStyle = hexToRgba(color, 0.15 + pulse * 0.15);
        ctx.lineWidth = 0.7 * sc;
        ctx.stroke();
        ctx.beginPath();
        ctx.arc(x2, y2, r + sc, 0, Math.PI * 2);
        ctx.strokeStyle = hexToRgba(color, 0.45);
        ctx.lineWidth = sc;
        ctx.stroke();
        ctx.beginPath();
        ctx.arc(x2, y2, r, 0, Math.PI * 2);
        ctx.fillStyle = color;
        ctx.shadowColor = color;
        ctx.shadowBlur = (10 + ratio * 8) * sc;
        ctx.fill();

        // 标签只在宽屏且比例足够大时显示
        const isHovered = this._hoveredCode === code;
        // 常驻显示：ratio 较高才显示，字体固定小号
        if ((ratio > 0.4 && !this.isMobile) || isHovered) {
            const fs = Math.round((isHovered ? 9 : 7 + ratio * 2) * sc);
            ctx.font = `600 ${fs}px system-ui,sans-serif`;
            ctx.textAlign = 'center';
            ctx.textBaseline = 'bottom';
            ctx.shadowBlur = 4;
            ctx.shadowColor = 'rgba(0,0,0,0.9)';
            ctx.fillStyle = isHovered ? 'rgba(255,255,255,1)' : 'rgba(255,255,255,0.75)';
            ctx.fillText(code, x2, y2 - r - 2 * sc);
        }
        ctx.restore();
    }

    // 出发地
    drawOrigin(now) {
        const { ctx, ox, oy } = this;
        const sc = this.scale || 1;
        const pulse = 0.5 + 0.5 * Math.sin(now * 0.004);
        ctx.save();
        for (const [radius, alpha] of [
            [(16 + pulse * 8) * sc, 0.06],
            [10 * sc, 0.15],
            [6 * sc, 0.3],
        ]) {
            ctx.beginPath();
            ctx.arc(ox, oy, radius, 0, Math.PI * 2);
            ctx.strokeStyle = `rgba(255,255,255,${alpha})`;
            ctx.lineWidth = sc;
            ctx.stroke();
        }
        ctx.beginPath();
        ctx.arc(ox, oy, 3.5 * sc, 0, Math.PI * 2);
        ctx.fillStyle = '#fff';
        ctx.shadowColor = '#fff';
        ctx.shadowBlur = 14 * sc;
        ctx.fill();
        ctx.restore();
    }

    // 主渲染循环
    draw(now) {
        const isVisible = this._visibilityFn
            ? this._visibilityFn()
            : document.getElementById('tab-geo')?.classList.contains('active');

        if (!isVisible) {
            if (this._visibilityFn) {
                // admin 摘要卡片模式：真正停止循环，由 resume() 重启
                this.raf = null;
                return;
            } else {
                // 完整报告页模式：保持原有行为，跳帧但继续调度
                this.raf = requestAnimationFrame(t => this.draw(t));
                return;
            }
        }

        if (!this.t0) this.t0 = now;
        const elapsed = now - this.t0;
        const { ctx, W, H } = this;
        ctx.clearRect(0, 0, W, H);
        this.drawBg();
        this.drawOrigin(now);
        for (const arc of this.arcs) {
            const dt = elapsed - arc.delay;
            arc._prog = dt <= 0 ? 0 : Math.min(1, dt / arc.drawDur);
            this.drawArc(arc, arc._prog);
            this.drawDest(arc, arc._prog, now);
        }
        for (const arc of this.arcs) { this.drawPlane(arc, arc._prog, now); }
        this.raf = requestAnimationFrame(t => this.draw(t));
    }

    _startAnim() { this.raf = requestAnimationFrame(t => this.draw(t)); }

    pause() {
        if (this.raf) { cancelAnimationFrame(this.raf); this.raf = null; }
    }

    resume() {
        if (!this.raf) { this.t0 = null; this._startAnim(); }
    }

    destroy() {
        if (this.raf) cancelAnimationFrame(this.raf);
        if (this._ro) this._ro.disconnect();
        this.canvas.removeEventListener('mousemove', this._onMouseMove);
        this.canvas.removeEventListener('mouseleave', this._onMouseLeave);
    }
}

// 根据时区推测出发地
function guessOrigin() {
    const offset = -new Date().getTimezoneOffset() / 60;
    if (offset >= 8) return { lon: 116.4, lat: 39.9 };  // 中国
    if (offset >= 5) return { lon: 80.0, lat: 20.0 };  // 南亚
    if (offset >= 3) return { lon: 45.0, lat: 35.0 };  // 中东
    if (offset >= 0) return { lon: 10.0, lat: 51.0 };  // 西欧
    if (offset >= -5) return { lon: -74.0, lat: 40.7 };// 美东
    return { lon: -118.2, lat: 34.0 };                 // 美西
}

let _geoMapInstance = null;

// ── theme ──
(function () {
    const saved = localStorage.getItem('theme');
    const prefer = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    applyTheme(saved || prefer);
})();
function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
    const isDark = theme === 'dark';
    const moon = document.getElementById('iconMoon'), sun = document.getElementById('iconSun'), label = document.getElementById('themeLabel');
    if (moon) moon.style.display = isDark ? 'block' : 'none';
    if (sun) sun.style.display = isDark ? 'none' : 'block';
    if (label) label.textContent = isDark ? '深色' : '浅色';
    const btn = document.getElementById('themeToggle');
    if (btn) btn.setAttribute('aria-pressed', String(isDark));
}
document.getElementById('themeToggle')?.addEventListener('click', () => {
    applyTheme(document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
});

const STORAGE_KEY = 'subscheck_api_key';
function getKey() { try { return localStorage.getItem(STORAGE_KEY) || sessionStorage.getItem(STORAGE_KEY) || null; } catch { return null; } }

async function sfetch(url, opts = {}) {
    const key = getKey();
    if (!key) return { ok: false, status: 401, error: '未认证' };
    opts.headers = { ...opts.headers, 'X-API-Key': key };
    try {
        const r = await fetch(url, opts);
        const ct = r.headers.get('content-type') || '';
        const text = await r.text();
        const payload = ct.includes('application/json') ? JSON.parse(text) : text;
        if (r.status === 401) { try { localStorage.removeItem(STORAGE_KEY); } catch { } showLoginArea('API 密钥已失效，请重新输入。'); return { ok: false, status: 401, payload }; }
        return r.ok ? { ok: true, status: r.status, payload } : { ok: false, status: r.status, payload };
    } catch (e) { return { ok: false, error: e.message }; }
}

function showScene(n) { document.getElementById('loginScene').style.display = n === 'login' ? 'flex' : 'none'; document.getElementById('reportScene').style.display = n === 'report' ? 'flex' : 'none'; }
function showLoading() { showScene('login'); _setBadge('Analysis Report'); document.getElementById('placeholderMsg').style.display = ''; document.getElementById('inlineLoginArea').style.display = 'none'; document.getElementById('retryArea').style.display = 'none'; }
function showLoginArea(hint = '') {
    showScene('login'); _setBadge('Auth Required');
    document.getElementById('placeholderMsg').style.display = 'none'; document.getElementById('inlineLoginArea').style.display = ''; document.getElementById('retryArea').style.display = 'none';
    const input = document.getElementById('inlineApiKey'), hintEl = document.getElementById('loginHint');
    if (hint) { input.classList.add('error'); hintEl.textContent = hint; } else { input.classList.remove('error'); hintEl.textContent = ''; input.placeholder = '输入 API 密钥...'; }
    setTimeout(() => input.focus(), 50);
}
function showRetryArea(msg, badge = 'Error') { showScene('login'); _setBadge(badge); document.getElementById('placeholderMsg').style.display = 'none'; document.getElementById('inlineLoginArea').style.display = 'none'; document.getElementById('retryArea').style.display = ''; document.getElementById('retryMsg').textContent = msg; }
function _setBadge(t) { document.getElementById('placeholderBadge').textContent = t; }

function switchTab(name) {
    document.querySelectorAll('.sidebar-btn').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.toggle('active', c.id === 'tab-' + name));
    // 切入地图标签时重置动画，让弧线重新从 0 绘制
    if (name === 'geo' && _geoMapInstance) { _geoMapInstance.t0 = null; }
}

async function inlineLogin() {
    const input = document.getElementById('inlineApiKey'), btn = document.getElementById('inlineLoginBtn'), hintEl = document.getElementById('loginHint');
    const k = input?.value?.trim();
    input.classList.remove('error'); hintEl.textContent = '';
    if (!k) { input.classList.add('error'); hintEl.textContent = '请输入 API 密钥'; input.focus(); return; }
    btn.disabled = true;
    try {
        const resp = await fetch('/api/status', { headers: { 'X-API-Key': k } });
        if (resp.status === 401) { input.classList.add('error'); hintEl.textContent = 'API 密钥无效，请重试'; input.value = ''; input.focus(); return; }
        if (!resp.ok) { showRetryArea(`验证失败（HTTP ${resp.status}），请检查服务状态。`); return; }
        try { localStorage.setItem(STORAGE_KEY, k); } catch { }
        await loadReport();
    } catch (e) { showRetryArea(`网络错误：${e.message}`); }
    finally { btn.disabled = false; }
}

async function loadReport() {
    showLoading();
    const [reportResult, cfgResult] = await Promise.all([sfetch('/api/analysis-report'), sfetch('/api/config')]);
    if (!reportResult.ok) { if (reportResult.status !== 401) showRetryArea(`加载失败（HTTP ${reportResult.status}），请检查服务是否正常。`); return; }
    if (!reportResult.payload?.report) { showRetryArea('尚未生成检测报告，请先在主界面运行一次节点检测。', 'No Data'); return; }
    try {
        const report = YAML.parse(reportResult.payload.report);
        let cfg = {};
        if (cfgResult.ok) {
            try {
                const rawCfg = typeof cfgResult.payload?.content === 'string'
                    ? cfgResult.payload.content
                    : (typeof cfgResult.payload === 'string' ? cfgResult.payload : '');
                if (rawCfg) cfg = YAML.parse(rawCfg) || {};
            } catch (e) { console.warn('config parse failed:', e); }
        }
        renderReport(report, cfg);
        showScene('report');
    } catch (e) { showRetryArea(`报告解析失败：${e.message}`); }
}

function doLogout() { try { localStorage.removeItem(STORAGE_KEY); } catch { } showLoginArea(); }
async function initPage() { if (!getKey()) { showLoginArea(); return; } await loadReport(); }
window.addEventListener('storage', e => { if (e.key === STORAGE_KEY && !e.newValue) showLoginArea('已在其他页面退出登录，请重新输入。'); });

// 正确解析格式化数字，支持"万"/"k"以及千分位逗号
function parseCount(raw) {
    const s = String(raw || '').trim().replace(/,|，/g, '');
    if (/万/.test(s)) { const n = parseFloat(s.replace('万', '')); return isNaN(n) ? 0 : Math.round(n * 10000); }
    if (/k$/i.test(s)) { const n = parseFloat(s); return isNaN(n) ? 0 : Math.round(n * 1000); }
    const n = parseInt(s, 10); return isNaN(n) ? 0 : n;
}

// 格式化函数
function fmtRate(r) {
    if (r === 0) return '0%';
    if (r < 0.1) return '<0.1%';
    if (r < 1) return r.toFixed(1) + '%';
    return Math.round(r) + '%';
}

let _reportData = null;

function renderReport(r, cfg) {
    _reportData = r;
    const ci = r.check_info || {}, ga = r.global_analysis || {}, sr = r.subs_ranking || [], sb = r.subs_ranking_bad || [];
    document.getElementById('navMeta').textContent = ci.check_time ? `${ci.check_time}  ·  耗时 ${ci.check_duration || '-'}` : '分析报告';
    const geoCount = Object.keys(ga.geography_distribution || {}).length, protoCount = Object.keys(ga.protocol_distribution || {}).length;
    _sb('sb-geo', geoCount); _sb('sb-proto', protoCount); _sb('sb-subs', sr.length);
    renderOverview(r, ci, ga, sr.length, geoCount, protoCount, cfg);
    renderGeo(ga); renderProto(ga); renderSubs(sr, sb, cfg); renderConfig(ci, ga, sr, sb, cfg);
}
function _sb(id, val) { const el = document.getElementById(id); if (el) el.textContent = val; }

function buildCfgStatusPanel(cfg, ci) {
    cfg = cfg || {};
    const SVG_OK = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;
    const SVG_WARN = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`;
    const SVG_DASH = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="5" y1="12" x2="19" y2="12"/></svg>`;

    const ghProxy = cfg['github-proxy'] || '';
    const recipientUrl = cfg['recipient-url'];
    const speedTestUrl = cfg['speed-test-url'] || '';
    const mediaCheck = cfg['media-check'] !== false;
    const keepSuccess = cfg['keep-success-proxies'] !== false;
    const autoUpdate = cfg['update'] !== false;
    const minSpeed = parseInt(cfg['min-speed']) || 0;
    const dlTimeout = parseInt(cfg['download-timeout']) || 0;
    const saveMethod = cfg['save-method'] || 'local';
    const cronExpr = cfg['cron-expression'] || '';
    const checkInterval = parseInt(cfg['check-interval']) || 0;

    let hasNotify = false;
    if (Array.isArray(recipientUrl)) hasNotify = recipientUrl.filter(v => typeof v === 'string' && v.trim()).length > 0;
    else if (typeof recipientUrl === 'string') hasNotify = recipientUrl.trim().length > 0;

    const scheduleStr = cronExpr ? `cron: ${cronExpr}` : (checkInterval ? `${checkInterval} 分钟` : '未设置');

    const rows = [
        ['GitHub 代理', ghProxy ? esc(ghProxy.replace(/https?:\/\//, '').replace(/\/$/, '')) : '未设置', ghProxy ? 'ok' : 'warn', ghProxy ? SVG_OK : SVG_WARN],
        ['通知渠道', hasNotify ? '已配置' : '未配置', hasNotify ? 'ok' : 'warn', hasNotify ? SVG_OK : SVG_WARN],
        ['流媒体检测', mediaCheck ? '已开启' : '已关闭', mediaCheck ? 'ok' : 'warn', mediaCheck ? SVG_OK : SVG_WARN],
        ['测速功能', speedTestUrl ? '已启用' : '已关闭', speedTestUrl ? 'ok' : 'warn', speedTestUrl ? SVG_OK : SVG_WARN],
        ['最低速度', minSpeed > 0 ? `${minSpeed} KB/s` : '未设置', minSpeed > 0 ? 'ok' : 'warn', minSpeed > 0 ? SVG_OK : SVG_WARN],
        ['测速超时', dlTimeout > 0 ? `${dlTimeout}s` : '未设置', dlTimeout > 0 ? 'ok' : 'warn', dlTimeout > 0 ? SVG_OK : SVG_WARN],
        ['保留成功节点', keepSuccess ? '已开启' : '已关闭', keepSuccess ? 'ok' : 'warn', keepSuccess ? SVG_OK : SVG_WARN],
        ['自动更新', autoUpdate ? '已开启' : '已关闭', autoUpdate ? 'ok' : 'warn', autoUpdate ? SVG_OK : SVG_WARN],
        ['存储方式', saveMethod, saveMethod !== 'local' ? 'ok' : 'muted-v', SVG_DASH],
        ['检测周期', scheduleStr, scheduleStr !== '未设置' ? 'ok' : 'warn', scheduleStr !== '未设置' ? SVG_OK : SVG_WARN],
    ];

    return `
        <div class="stat-panel">
            <div class="panel-title">关键配置状态</div>
            ${rows.map(([k, v, cls, icon]) => `
                <div class="cfg-status-row">
                    <span class="cfg-status-k">${k}</span>
                    <span class="cfg-status-v ${cls}">${icon}${v}</span>
                </div>`).join('')}
        </div>`;
}

function renderOverview(r, ci, ga, subCount, geoCount, protoCount, cfg) {
    cfg = cfg || {};
    document.getElementById('heroSummary').innerHTML =
        `<div class="hero-summary"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg><div class="hero-summary-text">${buildHeroSentence(ci, ga)}</div></div>`;

    const minSpeedVal = ci.check_min_speed;
    const minSpeedStr = (minSpeedVal && String(minSpeedVal) !== '0') ? minSpeedVal + ' KB/s' : '未开启';
    const total = parseCount(ga.alive_count);
    const checked = parseCount(ci.check_count_raw);
    const qm = ga.quality_metrics || {}, cfd = qm.cf_details || {};
    const cfCon = cfd['consistent_¹⁺'] || {}, cfIncon = cfd['inconsistent_⁰'] || {};
    let vps = {};
    for (const [k, v] of Object.entries(qm)) { if (k.startsWith('vps_details') && typeof v === 'object') { vps = v; break; } }
    const cfConTotal = Object.values(cfCon).reduce((a, b) => a + b, 0);
    const vpsTotal = Object.values(vps).reduce((a, b) => a + b, 0);

    const passRate = checked > 0 ? Math.min(100, total / checked * 100) : 0;
    const chips = [
        { label: '可用节点', value: total, sub: `共检测 ${ci.check_count_raw || '-'}`, icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>`, color: 'var(--accent)' },
        { label: '通过率', value: fmtRate(passRate), sub: `速度下限 ${minSpeedStr}`, icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>`, color: passRate >= 5 ? 'var(--success)' : passRate > 0 ? 'var(--warning)' : 'var(--danger)' },
        { label: '覆盖地区', value: geoCount, sub: '国家/地区', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>`, color: 'var(--idle)' },
        { label: '协议种类', value: protoCount, sub: '协议分布', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>`, color: 'var(--idle)' },
        { label: 'CF 中转', value: cfConTotal, sub: total ? Math.round(cfConTotal / total * 100) + '%' : '0%', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>`, color: 'var(--success)' },
        { label: '独立 VPS', value: vpsTotal, sub: total ? Math.round(vpsTotal / total * 100) + '%' : '0%', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>`, color: 'var(--warning)' },
        { label: '流量消耗', value: ci.check_traffic || '-', sub: `耗时 ${ci.check_duration || '-'}`, icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>`, color: 'var(--muted)' },
        { label: '活跃订阅', value: subCount, sub: `检测时间 ${ci.check_time || '-'}`, icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>`, color: 'var(--muted)' },
    ];
    document.getElementById('summaryChips').innerHTML = chips.map(c =>
        `<div class="summary-chip"><div class="chip-icon" style="background:color-mix(in srgb,${c.color} 12%,transparent);color:${c.color}">${c.icon}</div><div class="chip-value" style="color:${c.color}">${c.value}</div><div class="chip-label">${c.label}</div><div class="chip-sub">${c.sub || ''}</div></div>`
    ).join('');

    // 解锁标签
    const unlockData = parseUnlockFromSummary((r.summary || '').trim());
    document.getElementById('unlockSection').innerHTML = unlockData.length
        ? `<div class="section-title" style="margin-bottom:8px">媒体 &amp; AI 解锁</div><div class="unlock-row" style="margin-bottom:16px">${unlockData.map(u => `<span class="unlock-tag"><span class="ut-dot" style="background:${u.color}"></span>${u.name}<span class="ut-count">${u.count}</span></span>`).join('')}</div>` : '';

    // 配置快览面板
    const cfgPanel = buildCfgStatusPanel(cfg, ci);

    // 线路质量面板
    const cfRatio = qm.cf_consistent_ratio || `${Math.round(cfConTotal / Math.max(1, total) * 100)}%`;
    const vpsRatioStr = `${Math.round(vpsTotal / Math.max(1, total) * 100)}%`;
    const cfBlock = cfd['blocked_⁻¹'] || {};
    const qualityPanel = `
        <div class="stat-panel">
            <div class="panel-title">线路质量</div>
            <div class="quality-row">
                <div class="quality-item"><div class="quality-val" style="color:var(--success)">${cfRatio}</div><div class="quality-label">CF 中转 ¹⁺</div></div>
                <div class="quality-item"><div class="quality-val" style="color:var(--idle)">${vpsRatioStr}</div><div class="quality-label">独立 VPS ²</div></div>
            </div>
            <div class="cf-breakdown">
                ${cfGroup('CF 一致 ¹⁺', cfCon, 'var(--success)')}
                ${cfGroup('CF 不一致 ⁰', cfIncon, 'var(--warning)')}
                ${Object.keys(cfBlock).length ? cfGroup('CF 异常 ⁻¹', cfBlock, 'var(--danger)') : ''}
                ${cfGroup('独立 VPS ²', vps, 'var(--idle)')}
            </div>
        </div>`;

    // 快速导航面板
    const navPanel = `
        <div class="stat-panel">
            <div class="panel-title">快速导航</div>
            ${[
            ['geo', '地理分布', `${geoCount} 个地区`, `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>`],
            ['proto', '协议分析', `${protoCount} 种协议`, `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>`],
            ['subs', '订阅排名', `${subCount} 个活跃`, `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>`],
            ['cfgana', '配置分析', '运行参数', `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>`],
        ].map(([tab, label, sub, icon]) =>
            `<button class="nav-card" onclick="switchTab('${tab}')"><div class="nav-card-left">${icon}<span class="nav-card-label">${label}</span></div><div class="nav-card-right">${sub}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg></div></button>`
        ).join('')}
        </div>`;

    document.getElementById('overviewExtra').innerHTML = `
        <div class="section-title" style="margin-top:4px">概况</div>
        <div class="overview-grid-3">${qualityPanel}${cfgPanel}${navPanel}</div>`;
}

function buildHeroSentence(ci, ga) {
    const total = ga.alive_count || 0, geo = Object.keys(ga.geography_distribution || {}).length;
    const qm = ga.quality_metrics || {}, cfRatio = qm.cf_consistent_ratio || '';
    const speed = ci.check_min_speed, speedStr = (speed && String(speed) !== '0') ? `，速度下限 <span class="hl">${speed} KB/s</span>` : '';
    return `本次检测耗时 <span class="hl">${ci.check_duration || '-'}</span>，消耗流量 <span class="hl">${ci.check_traffic || '-'}</span>，共检测 <span class="hl">${ci.check_count_raw || '-'}</span> 个节点${speedStr}，获得 <span class="hl-ok">${total}</span> 个可用节点，覆盖 <span class="hl">${geo}</span> 个国家/地区` + (cfRatio ? `，CF 中转占比 <span class="hl">${cfRatio}</span>` : '') + '。';
}

function parseUnlockFromSummary(text) {
    const colors = { 'Netflix': '#e50914', 'YouTube': '#ff0000', 'Disney+': '#113ccf', 'TikTok': '#010101', 'GPT+': '#10a37f', 'GPT': '#10a37f', 'Gemini': '#4285f4' };
    const result = [];
    for (const m of [text.match(/流媒体解锁:\s*\[([^\]]*)\]/), text.match(/AI 解锁\[([^\]]*)\]/)]) {
        if (!m) continue;
        for (const item of m[1].split(',').map(s => s.trim()).filter(Boolean)) {
            const [name, count] = item.split(':').map(s => s.trim());
            if (name && count) result.push({ name, count, color: colors[name] || 'var(--muted)' });
        }
    }
    return result;
}

function cfGroup(title, obj, color) {
    const entries = Object.entries(obj).sort((a, b) => b[1] - a[1]); if (!entries.length) return '';
    return `<div class="cf-group"><span class="cf-group-title" style="color:${color}">${title}</span><span class="cf-tags">${entries.map(([k, v]) => `<span class="cf-tag">${k}<em>${v}</em></span>`).join('')}</span></div>`;
}

// 地区映射
const GEO_REGIONS = {
    // ── 亚洲 (Asia) ──
    CN: '亚洲', HK: '亚洲', TW: '亚洲', JP: '亚洲', KR: '亚洲', MN: '亚洲', MO: '亚洲', KP: '亚洲',
    SG: '亚洲', TH: '亚洲', MY: '亚洲', ID: '亚洲', VN: '亚洲', PH: '亚洲', MM: '亚洲', KH: '亚洲',
    LA: '亚洲', BN: '亚洲', TL: '亚洲', IN: '亚洲', BD: '亚洲', PK: '亚洲', LK: '亚洲', NP: '亚洲',
    AF: '亚洲', BT: '亚洲', MV: '亚洲', KZ: '亚洲', UZ: '亚洲', TM: '亚洲', KG: '亚洲', TJ: '亚洲',
    GE: '亚洲', AM: '亚洲', AZ: '亚洲',

    // ── 中东 (Middle East) ──
    TR: '中东', IL: '中东', AE: '中东', SA: '中东', QA: '中东', KW: '中东', IQ: '中东', IR: '中东',
    JO: '中东', LB: '中东', OM: '中东', BH: '中东', YE: '中东', SY: '中东', PS: '中东',

    // ── 欧洲 (Europe) ──
    GB: '欧洲', FR: '欧洲', DE: '欧洲', NL: '欧洲', BE: '欧洲', LU: '欧洲', CH: '欧洲', AT: '欧洲',
    IE: '欧洲', PT: '欧洲', ES: '欧洲', IT: '欧洲', GR: '欧洲', MT: '欧洲', CY: '欧洲', MC: '欧洲',
    LI: '欧洲', AD: '欧洲', SM: '欧洲', VA: '欧洲', SE: '欧洲', NO: '欧洲', DK: '欧洲', FI: '欧洲',
    IS: '欧洲', RU: '欧洲', UA: '欧洲', BY: '欧洲', MD: '欧洲', LT: '欧洲', LV: '欧洲', EE: '欧洲',
    PL: '欧洲', CZ: '欧洲', SK: '欧洲', HU: '欧洲', RO: '欧洲', BG: '欧洲', HR: '欧洲', RS: '欧洲',
    SI: '欧洲', BA: '欧洲', MK: '欧洲', AL: '欧洲', ME: '欧洲', XK: '欧洲',

    // ── 北美 (North America & Caribbean) ──
    US: '北美', CA: '北美', MX: '北美', GT: '北美', BZ: '北美', HN: '北美', SV: '北美', NI: '北美',
    CR: '北美', PA: '北美', CU: '北美', JM: '北美', HT: '北美', DO: '北美', PR: '北美', TT: '北美',
    BB: '北美',

    // ── 南美 (South America) ──
    CO: '南美', VE: '南美', GY: '南美', SR: '南美', BR: '南美', EC: '南美', PE: '南美', BO: '南美',
    PY: '南美', UY: '南美', AR: '南美', CL: '南美',

    // ── 非洲 (Africa) ──
    MA: '非洲', DZ: '非洲', TN: '非洲', LY: '非洲', EG: '非洲', SD: '非洲', SS: '非洲', MR: '非洲',
    SN: '非洲', GM: '非洲', GW: '非洲', GN: '非洲', SL: '非洲', LR: '非洲', CI: '非洲', GH: '非洲',
    TG: '非洲', BJ: '非洲', NG: '非洲', BF: '非洲', ML: '非洲', NE: '非洲', CM: '非洲', CF: '非洲',
    TD: '非洲', GQ: '非洲', GA: '非洲', CG: '非洲', CD: '非洲', AO: '非洲', ET: '非洲', ER: '非洲',
    DJ: '非洲', SO: '非洲', KE: '非洲', UG: '非洲', RW: '非洲', BI: '非洲', TZ: '非洲', MZ: '非洲',
    MW: '非洲', ZM: '非洲', ZW: '非洲', MG: '非洲', ZA: '非洲', NA: '非洲', BW: '非洲', LS: '非洲',
    SZ: '非洲',

    // ── 大洋洲 (Oceania) ──
    AU: '大洋洲', NZ: '大洋洲', PG: '大洋洲', FJ: '大洋洲', SB: '大洋洲', VU: '大洋洲', WS: '大洋洲',
    TO: '大洋洲', KI: '大洋洲', FM: '大洋洲', PW: '大洋洲',
};

const REGION_COLORS = {
    '亚洲': '#0ea5a0',
    '欧洲': '#7c3aed',
    '北美': '#2563eb',
    '南美': '#059669',
    '中东': '#d97706',
    '非洲': '#dc2626',
    '大洋洲': '#db2777',
    '其他': '#64748b',
};

// accent 渐变色列表（前 10 名用彩色，其余统一用 muted）
const GEO_TOP_COLORS = [
    '#0ea5a0', '#7c3aed', '#2563eb', '#d97706', '#db2777',
    '#059669', '#0891b2', '#be185d', '#dc2626', '#64748b',
];

function renderGeo(ga) {
    const geoEl = document.getElementById('geoBars');
    geoEl.style.cssText = 'display:block';

    const geo = ga.geography_distribution || {};
    const entries = Object.entries(geo).sort((a, b) => b[1] - a[1]);
    if (!entries.length) {
        geoEl.innerHTML = '<div style="padding:16px;color:var(--muted);font-size:13px">暂无地区数据</div>';
        return;
    }

    const geoTotal = entries.reduce((a, [, v]) => a + v, 0) || 1;
    const maxVal = entries[0]?.[1] || 1;
    const top10 = entries.slice(0, 10);
    const rest = entries.slice(10);

    // 大区汇总
    const regionMap = {};
    for (const [k, v] of entries) {
        const r = GEO_REGIONS[k] || '其他';
        regionMap[r] = (regionMap[r] || 0) + v;
    }
    const regionEntries = Object.entries(regionMap).sort((a, b) => b[1] - a[1]);

    const regionBar = regionEntries.map(([r, v]) =>
        `<div class="geo-region-seg" style="flex:${Math.max(1, v)};background:${REGION_COLORS[r] || '#64748b'}" title="${r}: ${v}"></div>`
    ).join('');

    const regionLegend = regionEntries.map(([r, v]) =>
        `<div class="geo-region-item"><span class="geo-region-dot" style="background:${REGION_COLORS[r] || '#64748b'}"></span><strong>${r}</strong>&nbsp;${v}&nbsp;<span style="opacity:.6">${Math.round(v / geoTotal * 100)}%</span></div>`
    ).join('');

    // Top 10
    const topRows = top10.map(([k, v], i) => {
        const pct = Math.round(v / geoTotal * 100);
        const color = GEO_TOP_COLORS[i] || '#64748b';
        return `<div class="geo-top-row">
            <span class="geo-top-rank">${i + 1}</span>
            <span class="geo-top-key">${k}</span>
            <div class="geo-top-bar-wrap"><div class="geo-top-bar" style="width:${Math.round(v / maxVal * 100)}%;background:${color}"></div></div>
            <span class="geo-top-val">${v}</span>
            <span class="geo-top-pct">${pct}%</span>
        </div>`;
    }).join('');

    // 其余国家两列
    let restHTML = '';
    if (rest.length) {
        const half = Math.ceil(rest.length / 2);
        const restMax = rest[0]?.[1] || 1;
        const restRow = ([k, v]) => {
            const pct = Math.round(v / geoTotal * 100);
            return `<div class="geo-all-row">
                <span class="geo-all-key">${k}</span>
                <div class="geo-all-bar-wrap"><div class="geo-all-bar" style="width:${Math.round(v / restMax * 100)}%;background:var(--border);filter:brightness(1.6)"></div></div>
                <span class="geo-all-val">${v}</span>
                <span class="geo-all-pct">${pct}%</span>
            </div>`;
        };
        restHTML = `
            <div style="border-top:1px solid var(--border);margin-top:6px">
                <div class="geo-section-title">其他地区（${rest.length} 个）</div>
                <div class="geo-all-grid">
                    <div class="geo-all-col">${rest.slice(0, half).map(restRow).join('')}</div>
                    <div class="geo-all-col">${rest.slice(half).map(restRow).join('')}</div>
                </div>
            </div>`;
    }

    // 摘要行
    const top3 = entries.slice(0, 3).map(([k, v]) =>
        `<strong>${k}</strong> ${Math.round(v / geoTotal * 100)}%`
    ).join(' &ensp;·&ensp; ');

    geoEl.innerHTML = `
        <div style="padding:14px 16px 0">
            <div class="section-title" style="margin-bottom:8px">大区分布</div>
            <div class="geo-region-bar">${regionBar}</div>
            <div class="geo-region-legend">${regionLegend}</div>
            <div class="section-title" style="margin-bottom:8px">Top ${top10.length}</div>
            <div class="geo-top-list">${topRows}</div>
        </div>
        ${restHTML}`;

    const geoView = document.querySelector('.geo-view');
    if (geoView) {
        const old = geoView.querySelector('.geo-summary');
        if (old) old.remove();
        geoView.insertAdjacentHTML('beforeend', `
            <div class="geo-summary">
                <div class="geo-summary-item-left">
                    <div class="geo-summary-item">共 <strong>${entries.length}</strong> 个地区</div>
                    <div class="geo-summary-item">节点数 <strong>${geoTotal}</strong></div>
                </div>
                <div class="geo-summary-item-right">前三：${top3}</div>
            </div>`);
    }

    // 飞行地图初始化
    const mapSlot = document.getElementById('mapSlot');
    if (mapSlot && entries.length) {
        mapSlot.style.display = 'flex';
        if (_geoMapInstance) { _geoMapInstance.destroy(); _geoMapInstance = null; }

        function _initMap(origin) {
            const hint = document.getElementById('mapOriginHint');
            if (hint && origin._fromGeo) {
                // 若已获取精确位置则提示坐标
                hint.textContent = `● ${origin.lat.toFixed(1)}°, ${origin.lon.toFixed(1)}°`;
            }
            _geoMapInstance = new GeoFlightMap(mapSlot, entries, origin);
        }

        // 先用时区猜测快速渲染，geolocation 成功后重建
        const guessed = guessOrigin();
        _initMap(guessed);

        if (navigator.geolocation) {
            navigator.geolocation.getCurrentPosition(
                pos => {
                    const precise = { lon: pos.coords.longitude, lat: pos.coords.latitude, _fromGeo: true };
                    // 若精确位置与猜测差距较大才重建
                    if (Math.hypot(precise.lon - guessed.lon, precise.lat - guessed.lat) > 3) {
                        if (_geoMapInstance) { _geoMapInstance.destroy(); }
                        _initMap(precise);
                    }
                },
                () => { },   // 拒绝授权静默失败，保留猜测位置
                { timeout: 4000, maximumAge: 60000 }
            );
        }
    }
}

const PROTO_COLORS = {
    vless: '#0ea5a0', vmess: '#d97706', trojan: '#7c3aed', ss: '#2563eb', ssr: '#1d4ed8',
    http: '#059669', socks5: '#64748b', hysteria: '#db2777', hy2: '#be185d', tuic: '#0891b2',
};
function getProtoColor(name) { return PROTO_COLORS[name.toLowerCase().replace(/[^a-z0-9]/g, '')] || '#94a3b8'; }

function buildDonutSVG(entries, total, size) {
    size = size || 110;
    const r = size * 0.38, cx = size / 2, cy = size / 2;
    const circumference = 2 * Math.PI * r;
    let offset = 0;
    const GAP = circumference * 0.008;

    const segments = entries.map(([k, v]) => {
        const frac = v / total;
        const dash = Math.max(0, circumference * frac - GAP);
        const color = getProtoColor(k);
        const seg = `<circle cx="${cx}" cy="${cy}" r="${r}"
            fill="none" stroke="${color}" stroke-width="${size * 0.13}"
            stroke-dasharray="${dash} ${circumference - dash}"
            stroke-dashoffset="${-offset}"
            stroke-linecap="butt"
            style="transition:stroke-dasharray .6s cubic-bezier(.4,0,.2,1)"/>`;
        offset += circumference * frac;
        return seg;
    });

    const inner = `<text x="${cx}" y="${cy - 4}" text-anchor="middle"
        font-size="${size * 0.17}" font-weight="800" fill="var(--fg)">${total}</text>
        <text x="${cx}" y="${cy + size * 0.13}" text-anchor="middle"
        font-size="${size * 0.1}" fill="var(--muted)">节点</text>`;

    return `<svg class="proto-donut-svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}" style="transform:rotate(-90deg)">
        <circle cx="${cx}" cy="${cy}" r="${r}" fill="none" stroke="var(--border)" stroke-width="${size * 0.13}"/>
        ${segments.join('')}
        <g style="transform:rotate(90deg);transform-origin:${cx}px ${cy}px">${inner}</g>
    </svg>`;
}

function renderProto(ga) {
    const proto = ga.protocol_distribution || {};
    const total = ga.alive_count || 1;
    const entries = Object.entries(proto).sort((a, b) => b[1] - a[1]);
    if (!entries.length) {
        document.getElementById('protoContent').innerHTML = '<p style="color:var(--muted);font-size:13px">暂无协议数据</p>';
        return;
    }

    const donutSVG = buildDonutSVG(entries, total, 110);
    const donutList = entries.map(([k, v]) => {
        const pct = Math.round(v / total * 100);
        const color = getProtoColor(k);
        return `<div class="proto-donut-row">
            <span class="proto-donut-dot" style="background:${color}"></span>
            <span class="proto-donut-name" style="color:${color}">${k.toUpperCase()}</span>
            <div class="proto-donut-bar-wrap"><div class="proto-donut-bar" style="width:${pct}%;background:${color}"></div></div>
            <span class="proto-donut-val">${v}</span>
            <span class="proto-donut-pct">${pct}%</span>
        </div>`;
    }).join('');

    // 顶部概要数据行
    const topProto = entries[0];
    const protoStatRow = `<div class="proto-stat-row">
        <div class="proto-stat-item"><div class="proto-stat-val">${total}</div><div class="proto-stat-label">节点总量</div></div>
        <div class="proto-stat-item"><div class="proto-stat-val">${entries.length}</div><div class="proto-stat-label">协议种类</div></div>
        ${topProto ? `<div class="proto-stat-item"><div class="proto-stat-val" style="color:${getProtoColor(topProto[0])}">${topProto[0].toUpperCase()}</div><div class="proto-stat-label">主力协议 ${Math.round(topProto[1] / total * 100)}%</div></div>` : ''}
        ${entries.length > 1 ? `<div class="proto-stat-item"><div class="proto-stat-val" style="color:${getProtoColor(entries[1][0])}">${entries[1][0].toUpperCase()}</div><div class="proto-stat-label">次要协议 ${Math.round(entries[1][1] / total * 100)}%</div></div>` : ''}
    </div>`;

    // 堆叠色条
    const stackBar = entries.map(([k, v]) =>
        `<div class="proto-summary-seg" style="flex:${Math.max(2, Math.round(v / total * 100))};background:${getProtoColor(k)}" title="${k}: ${v} (${Math.round(v / total * 100)}%)"></div>`
    ).join('');

    // 协议卡片
    const cards = entries.map(([k, v]) => {
        const pct = Math.round(v / total * 100);
        const color = getProtoColor(k);
        return `<div class="proto-card" style="--proto-color:${color}">
            <div class="proto-card-header">
                <span class="proto-card-name" style="color:${color}">${k}</span>
                <span class="proto-card-pct">${pct}%</span>
            </div>
            <div class="proto-card-count" style="color:${color}">${v}</div>
            <div class="proto-card-bar-wrap"><div class="proto-card-bar" style="width:${pct}%"></div></div>
            <div class="proto-card-pct" style="color:var(--muted)">共 ${total} 中占 ${v} 个</div>
        </div>`;
    }).join('');

    document.getElementById('protoContent').innerHTML = `
        <div class="section-title">协议总览</div>
        ${protoStatRow}
        <div class="proto-donut-wrap">${donutSVG}<div class="proto-donut-list">${donutList}</div></div>
        <div class="proto-summary-bar" style="margin-bottom:14px">${stackBar}</div>
        <div class="section-title">协议卡片</div>
        <div class="proto-grid">${cards}</div>`;
}

function renderSubs(subs, subsBad, cfg) {
    cfg = cfg || {};

    // 复制工具栏
    document.getElementById('copyToolbar').innerHTML = subs.length
        ? `<div class="copy-toolbar">
        <span class="copy-toolbar-label">复制：</span>
        <button class="copy-btn" id="copyUrlsBtn" onclick="copyUrls(false)" title="每行一个 URL">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          <span class="btn-text">URL 列表</span>
        </button>
        <button class="copy-btn" id="copyYamlBtn" onclick="copyUrls(true)" title="可直接替换 sub-urls 配置段">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
          <span class="btn-text">YAML 格式</span>
        </button>
        <div class="copy-scope">
          <label title="同时包含沉默订阅">
            <input type="checkbox" id="copyIncludeBad">
            <span class="scope-text">含沉默订阅</span>
          </label>
        </div>
      </div>`
        : '';

    if (!subs.length) {
        document.getElementById('rankingContent').innerHTML =
            '<p style="color:var(--muted);font-size:13px">暂无活跃订阅</p>';
    } else {
        // ① 先渲染列表 HTML
        const listHTML = subs.map((s, i) => {
            const stats = s.stats || {};
            const rateStr = String(stats.rate || '0%');
            const rateNum = parseFloat(rateStr);
            const barColor = rateNum >= 10 ? 'var(--success)' : rateNum > 0 ? 'var(--warning)' : 'var(--danger)';
            const locs = Array.isArray(s.top_locations) ? s.top_locations.join('').split('|').filter(Boolean) : [];
            const protos = s.protocols ? Object.entries(s.protocols).sort((a, b) => b[1] - a[1]) : [];
            const tierClass = rateNum >= 20 ? 'tier-s' : rateNum >= 10 ? 'tier-a' : rateNum >= 3 ? 'tier-b' : 'tier-c';
            const tierLabel = rateNum >= 20 ? 'S' : rateNum >= 10 ? 'A' : rateNum >= 3 ? 'B' : 'C';
            return `<div class="sub-item" data-rate="${rateNum}">
        <div class="sub-header">
          <span class="sub-rank">${i + 1}</span>
          <span class="sub-url" title="${esc(s.url)}">${esc(s.url)}</span>
          <span class="sub-tier ${tierClass}">${tierLabel}</span>
          <span class="sub-rate" style="color:${barColor}">${rateStr}</span>
        </div>
        <div class="sub-bar-wrap"><div class="sub-bar" style="width:${Math.min(rateNum, 100)}%;background:${barColor}"></div></div>
        <div class="sub-meta">
          <span>${stats.success || 0} / ${stats.total || 0} 节点</span>
          ${locs.map(l => `<span class="tag-pill">${l}</span>`).join('')}
          ${protos.map(([k, v]) => `<span class="tag-pill">${k}:${v}</span>`).join('')}
        </div>
      </div>`;
        }).join('');

        document.getElementById('rankingContent').innerHTML =
            `<div class="section-title">订阅排名（${subs.length} 个活跃）</div>
       <div id="thresholdSlot"></div>
       <div class="sub-list">${listHTML}</div>`;

        // ② 列表已在 DOM 中，再初始化手柄
        initThresholdSlider(subs, cfg);
    }

    // 沉默订阅
    if (!subsBad.length) {
        document.getElementById('badContent').innerHTML = '';
        return;
    }
    document.getElementById('badContent').innerHTML =
        `<div class="section-title-toggle" onclick="toggleBad()">
       沉默订阅&nbsp;<span style="font-weight:400;text-transform:none;letter-spacing:0">(${subsBad.length})</span>
       <span class="toggle-line"></span>
       <svg class="toggle-chevron" id="badChevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
     </div>
     <div id="badList" style="display:none">
       <div class="bad-list">
         ${subsBad.map(s => {
            const st = s.stats || {};
            return `<div class="bad-item">
             <span class="bad-url" title="${esc(s.url)}">${esc(s.url)}</span>
             <span class="bad-count">${st.success || 0}/${st.total || 0}</span>
           </div>`;
        }).join('')}
       </div>
     </div>`;
}

function drawRuler(svgEl, maxRate) {
    if (!svgEl) return;
    // 必须在 DOM 渲染后取真实宽度
    const W = svgEl.getBoundingClientRect().width || svgEl.parentElement?.clientWidth || 400;
    const H = 28;
    // 用真实像素坐标，不设 viewBox 缩放
    svgEl.setAttribute('viewBox', `0 0 ${W} ${H}`);
    svgEl.removeAttribute('preserveAspectRatio');

    const steps = 20;
    let html = `<line x1="0" y1="${H / 2}" x2="${W}" y2="${H / 2}" stroke="rgba(100,105,113,0.1)" stroke-width="0.5"/>`;

    for (let i = 0; i <= steps; i++) {
        const x = (i / steps) * W;
        const isMajor = i % 5 === 0;
        const tickH = isMajor ? 10 : 5;
        const y1 = (H - tickH) / 2;
        const y2 = y1 + tickH;
        html += `<line
            x1="${x.toFixed(1)}" y1="${y1}" x2="${x.toFixed(1)}" y2="${y2}"
            stroke="${isMajor ? 'rgba(14,165,160,0.4)' : 'rgba(100,105,113,0.22)'}"
            stroke-width="${isMajor ? 1.2 : 0.8}"/>`;
        if (isMajor) {
            const val = Math.round(i / steps * maxRate);
            const anchor = i === 0 ? 'start' : i === steps ? 'end' : 'middle';
            html += `<text
                x="${x.toFixed(1)}" y="${y2 + 9}"
                text-anchor="${anchor}"
                font-size="9"
                fill="rgba(100,105,113,0.5)"
                font-family="system-ui,-apple-system,sans-serif">${val}%</text>`;
        }
    }
    svgEl.innerHTML = html;
}

function initThresholdSlider(subs, cfg) {
    const slot = document.getElementById('thresholdSlot');
    if (!slot) return;

    const rates = subs.map(s => parseFloat(s.stats?.rate || '0'));
    const maxRate = Math.max(...rates, 1);
    const cfgRate = parseFloat(cfg['success-rate'] || '0') * 100; // 0.05 → 5%
    let threshold = cfgRate > 0 ? Math.min(cfgRate, maxRate) : 0;

    slot.innerHTML = `
    <div class="threshold-card">
      <div class="threshold-top">
        <div class="threshold-label">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="18" x2="21" y2="18"/>
          </svg>
          成功率筛选
        </div>
        <div class="threshold-chip hidden" id="thresholdChip">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="22 7 13.5 15.5 8.5 10.5 2 17"/><polyline points="16 7 22 7 22 13"/>
          </svg>
          <span id="thresholdChipVal"></span>
        </div>
      </div>
      <div class="ruler-wrap">
        <div class="ruler-track" id="rulerTrack">
          <div class="ruler-fill" id="rulerFill" style="width:0%"></div>
          <svg class="ruler-svg" id="rulerSvg"></svg>
          <div class="ruler-handle" id="rulerHandle" style="left:0%"></div>
          <div class="ruler-handle-hit" id="rulerHandleHit" style="left:0%"></div>
        </div>
      </div>
      <div class="threshold-foot">
        <span class="foot-above" id="thresholdAbove"></span>
        <span class="foot-below" id="thresholdBelow"></span>
      </div>
    </div>`;

    // 绘制刻度
    const svgEl = document.getElementById('rulerSvg');
    // rAF 确保 DOM 已渲染、元素已有实际宽度
    requestAnimationFrame(() => {
        drawRuler(svgEl, maxRate);

        // resize 时重绘（容器宽度变化时刻度不变形）
        const ro = new ResizeObserver(() => drawRuler(svgEl, maxRate));
        ro.observe(document.getElementById('rulerTrack'));
    });

    const track = document.getElementById('rulerTrack');
    const handle = document.getElementById('rulerHandle');
    const handleHit = document.getElementById('rulerHandleHit');
    const fill = document.getElementById('rulerFill');
    const chip = document.getElementById('thresholdChip');
    const chipVal = document.getElementById('thresholdChipVal');

    const updateUI = () => {
        const pct = Math.max(0, Math.min(1, threshold / maxRate));
        handle.style.left = `${pct * 100}%`;
        handleHit.style.left = `${pct * 100}%`;
        fill.style.width = `${pct * 100}%`;

        if (threshold <= 0) {
            chip.classList.add('hidden');
        } else {
            chip.classList.remove('hidden');
            chipVal.textContent = `≥ ${threshold.toFixed(1)}%`;
        }

        let above = 0, below = 0;
        document.querySelectorAll('.sub-item[data-rate]').forEach(el => {
            const r = parseFloat(el.dataset.rate);
            const dim = threshold > 0 && r < threshold;
            el.classList.toggle('sub-item--dim', dim);
            dim ? below++ : above++;
        });

        document.getElementById('thresholdAbove').textContent =
            threshold > 0 ? `${above} 个达标` : `${above} 个活跃订阅`;
        document.getElementById('thresholdBelow').textContent =
            threshold > 0 ? `${below} 个低于阈值` : '';

        const activeCount = document.querySelectorAll('.sub-item[data-rate]:not(.sub-item--dim)').length;
        const urlsBtn = document.getElementById('copyUrlsBtn');
        const yamlBtn = document.getElementById('copyYamlBtn');
        if (urlsBtn) urlsBtn.querySelector('.btn-text').textContent =
            activeCount < subs.length ? `URL 列表 (${activeCount})` : 'URL 列表';
        if (yamlBtn) yamlBtn.querySelector('.btn-text').textContent =
            activeCount < subs.length ? `YAML 格式 (${activeCount})` : 'YAML 格式';
    };

    const setFromX = clientX => {
        const rect = track.getBoundingClientRect();
        let pct = (clientX - rect.left) / rect.width;
        pct = Math.max(0, Math.min(1, pct));
        threshold = pct < 0.03 ? 0 : pct * maxRate;
        updateUI();
    };

    let dragging = false;
    handleHit.addEventListener('pointerdown', e => {
        dragging = true;
        handleHit.setPointerCapture(e.pointerId);
        handleHit.classList.add('dragging');
        handle.classList.add('dragging');
    });
    handleHit.addEventListener('pointermove', e => { if (dragging) setFromX(e.clientX); });
    handleHit.addEventListener('pointerup', () => {
        dragging = false;
        handleHit.classList.remove('dragging');
        handle.classList.remove('dragging');
    });
    track.addEventListener('click', e => { if (!dragging) setFromX(e.clientX); });

    updateUI();
}

const _copyTimers = {};
const _COPY_SVG_URL = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>`;
const _COPY_SVG_YML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>`;
const _COPY_SVG_OK = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;
const _COPY_SVG_ERR = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`;

function _resetBtn(id) {
    if (_copyTimers[id]) { clearTimeout(_copyTimers[id]); delete _copyTimers[id]; }
    const btn = document.getElementById(id); if (!btn) return;
    btn.classList.remove('copied');
    btn.innerHTML = id === 'copyUrlsBtn' ? `${_COPY_SVG_URL}<span class="btn-text">URL 列表</span>` : `${_COPY_SVG_YML}<span class="btn-text">YAML 格式</span>`;
}
function _flashBtn(id, ok) {
    if (_copyTimers[id]) { clearTimeout(_copyTimers[id]); delete _copyTimers[id]; }
    const btn = document.getElementById(id); if (!btn) return;
    if (ok) { btn.classList.add('copied'); btn.innerHTML = `${_COPY_SVG_OK}<span class="btn-text">已复制</span>`; _copyTimers[id] = setTimeout(() => _resetBtn(id), 1800); }
    else { btn.classList.remove('copied'); btn.innerHTML = `${_COPY_SVG_ERR}<span class="btn-text">请手动复制</span>`; _copyTimers[id] = setTimeout(() => _resetBtn(id), 2200); }
}
// copyUrls 改为只复制未被 dim 的条目
function copyUrls(asYaml) {
    if (!_reportData) return;
    const subs = _reportData.subs_ranking || [];
    const subsBad = _reportData.subs_ranking_bad || [];
    const includeBad = document.getElementById('copyIncludeBad')?.checked;
    const btnId = asYaml ? 'copyYamlBtn' : 'copyUrlsBtn';

    // 读取当前未被阈值过滤的订阅（保持原始顺序）
    const activeSubs = subs.filter((_, i) => {
        const el = document.querySelector(`.sub-item[data-rate]:nth-of-type(${i + 1})`);
        // 更可靠：直接查 DOM
        return true; // 见下方替代写法
    });

    // 更可靠：从 DOM 读取可见项的 URL
    const visibleUrls = new Set(
        [...document.querySelectorAll('.sub-item[data-rate]:not(.sub-item--dim) .sub-url')]
            .map(el => el.getAttribute('title')) // title 存的是原始 URL
    );
    const filteredSubs = subs.filter(s => visibleUrls.has(s.url));

    let text = '';
    if (asYaml) {
        const fmt = s => {
            const st = s.stats || {};
            const protos = s.protocols
                ? Object.entries(s.protocols).sort((a, b) => b[1] - a[1]).map(([k, v]) => `${k}: ${v}`).join('; ')
                : '';
            return `  - ${s.url} # ${String(st.rate || '0%')} (${st.success || 0}/${st.total || 0})${protos ? ' [' + protos + ']' : ''}`;
        };
        text = `# 活跃订阅列表\nsub-urls:\n` + filteredSubs.map(fmt).join('\n');
        if (includeBad && subsBad.length)
            text += `\n\n# 沉默订阅\nsub-urls-silent:\n` +
                subsBad.map(s => { const st = s.stats || {}; return `  - ${s.url} # 0.0% (${st.success || 0}/${st.total || 0})`; }).join('\n');
    } else {
        text = filteredSubs.map(s => s.url).join('\n');
        if (includeBad && subsBad.length)
            text += '\n\n# 沉默订阅\n' + subsBad.map(s => s.url).join('\n');
    }

    writeClipboard(text).then(ok => _flashBtn(btnId, ok));
}

function renderConfig(ci, ga, sr, sb, cfg) {
    const total = parseCount(ga.alive_count);
    const checked = parseCount(ci.check_count_raw);
    const speed = parseInt(String(ci.check_min_speed || '0').replace(/[^0-9]/g, '')) || 0;
    const badCnt = sb.length, goodCnt = sr.length;
    const qm = ga.quality_metrics || {};
    const passRate = checked > 0
        ? Math.min(100, total / checked * 100) : 0;
    const geoCount = Object.keys(ga.geography_distribution || {}).length;
    const protoCount = Object.keys(ga.protocol_distribution || {}).length;
    let vps = {}; for (const [k, v] of Object.entries(qm)) { if (k.startsWith('vps_details') && typeof v === 'object') { vps = v; break; } }
    const vpsTotal = Object.values(vps).reduce((a, b) => a + b, 0);

    const ghProxy = cfg['github-proxy'] || '';
    const recipientUrl = cfg['recipient-url'];
    const speedTestUrl = cfg['speed-test-url'] || '';
    const mediaCheck = cfg['media-check'] !== false;
    const nodePrefix = cfg['node-prefix'] || '';
    const autoUpdate = cfg['update'] !== false;
    const keepSuccess = cfg['keep-success-proxies'] !== false;
    const minSpeed = parseInt(cfg['min-speed']) || 0;
    const dlTimeout = parseInt(cfg['download-timeout']) || 0;
    const dlMb = parseInt(cfg['download-mb']) || 0;
    const checkInterval = parseInt(cfg['check-interval']) || 0;
    const cronExpr = cfg['cron-expression'] || '';
    const aliveCon = parseInt(cfg['alive-concurrent']) || 0;
    const speedCon = parseInt(cfg['speed-concurrent']) || 0;
    const mediaCon = parseInt(cfg['media-concurrent']) || 0;
    const successRate = parseFloat(cfg['success-rate']) || 0;
    const successLimit = parseInt(cfg['success-limit']) || 0;
    const nodeType = cfg['node-type'];
    const ispCheck = cfg['isp-check'] === true;
    const saveMethod = cfg['save-method'] || 'local';
    const sharePassword = cfg['share-password'] || '';
    const dropBadCf = cfg['drop-bad-cf-nodes'] === true;
    const updateOnStart = cfg['update-on-startup'] !== false;

    const subUrls = Array.isArray(cfg['sub-urls']) ? cfg['sub-urls'] : [];
    const hasLocalhostAll = subUrls.some(u => typeof u === 'string' && /127\.0\.0\.1.*all\.yaml/i.test(u));

    let hasNotify = false;
    if (Array.isArray(recipientUrl)) hasNotify = recipientUrl.filter(v => typeof v === 'string' && v.trim()).length > 0;
    else if (typeof recipientUrl === 'string') hasNotify = recipientUrl.trim().length > 0;

    // KV: 本次检测运行参数
    const kvs = [
        { k: '检测节点数', v: ci.check_count_raw || '-' },
        { k: '检测耗时', v: ci.check_duration || '-' },
        { k: '流量消耗', v: ci.check_traffic || '-' },
        { k: '速度下限', v: speed > 0 ? speed + ' KB/s' : '未设置', cls: speed > 0 ? 'ok' : 'warn' },
        { k: '可用节点', v: total, cls: total > 0 ? 'ok' : 'bad' },
        { k: '通过率', v: checked > 0 ? fmtRate(passRate) : '—', cls: passRate >= 1 ? 'ok' : passRate > 0 ? 'warn' : 'bad' },
        { k: '活跃订阅', v: goodCnt + ' 个' },
        { k: '沉默订阅', v: badCnt + ' 个', cls: badCnt > 0 ? 'warn' : 'ok' },
        { k: '检测时间', v: ci.check_time || '-' },
    ];

    // KV: 关键配置项
    const cfgKvs = [
        { k: 'Github 代理', v: ghProxy ? esc(ghProxy) : '未设置', cls: ghProxy ? 'ok' : 'warn', title: ghProxy ? ghProxy : undefined },
        { k: '通知渠道', v: hasNotify ? '已配置' : '未配置', cls: hasNotify ? 'ok' : 'warn' },
        { k: '流媒体检测', v: mediaCheck ? '开启' : '关闭', cls: mediaCheck ? 'ok' : 'warn' },
        { k: '测速功能', v: speedTestUrl ? '已启用' : '关闭', cls: speedTestUrl ? 'ok' : 'warn' },
        { k: '自动更新', v: autoUpdate ? '开启' : '关闭', cls: autoUpdate ? 'ok' : 'warn' },
        { k: '保留成功节点', v: keepSuccess ? '开启' : '关闭', cls: keepSuccess ? 'ok' : 'warn' },
        { k: '存储方式', v: saveMethod, cls: saveMethod === 'local' ? '' : 'ok' },
        { k: '节点前缀', v: nodePrefix ? esc(nodePrefix) : '无' },
    ];

    // KV: 并发 & 速度参数
    const concKvs = [
        { k: '测活并发', v: aliveCon > 0 ? aliveCon : '自动' },
        { k: '测速并发', v: speedCon > 0 ? speedCon : '自动' },
        { k: '媒体检测并发', v: mediaCon > 0 ? mediaCon : '自动' },
        { k: '最低测速', v: minSpeed > 0 ? minSpeed + ' KB/s' : '未设置', cls: minSpeed > 0 ? 'ok' : 'warn' },
        { k: '测速超时', v: dlTimeout > 0 ? dlTimeout + 's' : '未设置', cls: dlTimeout > 0 ? 'ok' : 'warn' },
        { k: '单节点限速', v: dlMb > 0 ? dlMb + ' MB' : '不限' },
        { k: '检测间隔', v: cronExpr ? cronExpr : (checkInterval ? checkInterval + '分钟' : '未设置') },
        { k: '节点数上限', v: successLimit > 0 ? successLimit + '个' : '不限' },
    ];

    const deployCards = [];
    if (!ghProxy) deployCards.push({ tab: 'advanced', title: 'Github 代理未设置', desc: '国内环境获取 GitHub 订阅源易超时，建议部署自有代理加速（或使用内置 ghproxy-group）。', actions: [{ label: 'CF-Proxy 一键部署', href: 'https://github.com/sinspired/CF-Proxy', primary: true }] });
    if (!hasNotify) deployCards.push({ tab: 'notify', title: '通知渠道未配置', desc: '配置 Apprise 后，检测完成自动推送到微信、Telegram、邮件等 100+ 渠道。', actions: [{ label: '3分钟快速配置', href: 'https://sinspired.github.io/apprise_vercel/docs/QuicSet', primary: true }, { label: '测试通知渠道', href: 'https://apprise.linkpc.dpdns.org', primary: false }] });
    if (!speedTestUrl) deployCards.push({ tab: 'subscriptions', title: '测速功能已关闭', desc: '设置 <code>speed-test-url</code> 启用真实下载测速，可精确过滤低速节点。建议使用自建测速地址避免被节点屏蔽。', actions: [{ label: '自建测速说明', href: 'https://github.com/sinspired/subs-check-pro#config', primary: false }] });

    //配置分析建议
    const suggests = [];
    // 1. 通过率
    if (checked > 0) {
        if (passRate === 0) suggests.push({ l: 'warn', t: '通过率为 0%，未获得任何可用节点。请检查订阅源可用性或网络连接。' });
        else if (passRate < 1) suggests.push({ l: 'warn', t: `通过率仅 ${fmtRate(passRate)}，低于正常水平。建议检查网络质量或订阅源状态。` });
        else if (passRate >= 5) suggests.push({ l: 'good', t: `通过率 ${fmtRate(passRate)}，节点筛选效果优秀。` });
    }
    // 2. 订阅活跃率（success-rate 影响活跃/无效分界线）
    const totalSubs = goodCnt + badCnt;
    if (successRate > 0) suggests.push({
        l: 'info',
        t: `<code>success-rate: ${(successRate * 100).toFixed(1)}%</code> 已设置，成功率低于此值的订阅会在日志中打印。`
    });
    if (badCnt > 0 && totalSubs > 0) {
        const r = Math.round(badCnt / totalSubs * 100);
        if (r > 50) suggests.push({ l: 'warn', t: `沉默订阅占比 ${r}%（${badCnt}/${totalSubs}），超半数沉默，建议清理或替换低质订阅源。` });
        else suggests.push({ l: 'info', t: `存在 ${badCnt} 个沉默订阅（${r}%），可前往"订阅排名"标签查看。` });
    } else if (badCnt === 0 && goodCnt > 0) {
        suggests.push({ l: 'good', t: '所有订阅源均活跃，订阅源状态良好。' });
    }

    // 3. 速度测试参数
    if (minSpeed === 0) suggests.push({ l: 'warn', tab: 'subscriptions', t: '<code>min-speed</code> 未设置，所有节点（含极慢节点）均被收录，建议设为 128–500 KB/s。' });
    else if (minSpeed > 2000) suggests.push({ l: 'warn', tab: 'subscriptions', t: `<code>min-speed: ${minSpeed} KB/s</code> 偏高，会对节点造成较大压力，建议降低至 500 KB/s 以内。` });
    else suggests.push({ l: 'good', t: `<code>min-speed: ${minSpeed} KB/s</code> 设置合理。` });
    if (!dlTimeout) suggests.push({ l: 'warn', tab: 'subscriptions', t: '<code>download-timeout</code> 未设置，测速无时间上限，极慢节点会阻塞测速队列，建议设为 10s。' });
    if (!dlMb) suggests.push({ l: 'info', tab: 'subscriptions', t: '<code>download-mb</code> 未限制单节点下载量，高并发时可能占满带宽，建议设为 20–50 MB。' });

    // 4. 检测间隔
    if (!cronExpr && checkInterval > 0) {
        if (checkInterval < 120) suggests.push({ l: 'warn', tab: 'schedule', t: `检测间隔 ${checkInterval} 分钟过于频繁，高频检测消耗流量大且易触发运营商阻断，建议设为 720 分钟（每天两次）或使用 <code>cron-expression</code>。` });
        else if (checkInterval >= 480) suggests.push({ l: 'good', t: `检测间隔 ${checkInterval} 分钟（约 ${Math.round(checkInterval / 60)} 小时），频率合理。` });
    }
    if (cronExpr) suggests.push({ l: 'good', t: `使用 cron 表达式 <code>${esc(cronExpr)}</code> 精确调度检测任务，推荐在网络空闲时段（凌晨/早晨）执行。` });

    // 5. keep-success-proxies + localhost all.yaml 冗余
    if (keepSuccess) {
        suggests.push({ l: 'good', t: '<code>keep-success-proxies: true</code> 已开启，可避免上游订阅更新时因暂时拉不到节点导致可用节点清空。' });
        if (hasLocalhostAll) suggests.push({ l: 'warn', t: '检测到 <code>sub-urls</code> 中包含 <code>127.0.0.1/all.yaml</code>。已启用 <code>keep-success-proxies</code> 时，程序会自动保留历史成功节点，无需再订阅本地 all.yaml，可移除该条目以减少冗余。' });
    } else {
        suggests.push({ l: 'info', tab: 'detection', t: '<code>keep-success-proxies: false</code>，每次检测不保留历史节点，上游订阅临时沉默时可用节点可能清零，建议设为 true。' });
    }
    // 6. 并发数
    if (aliveCon > 500) suggests.push({ l: 'warn', tab: 'detection', t: `测活并发 ${aliveCon} 过高，超出一般路由器芯片处理能力，建议设为 100–300，量力而行。` });
    else if (aliveCon === 0) suggests.push({ l: 'info', t: '测活并发已设为自动，程序将根据 <code>concurrent</code> 基准自动计算，首次运行后可根据实际情况调整。' });
    if (speedCon > 32) suggests.push({ l: 'warn', tab: 'detection', t: `测速并发 ${speedCon} 较高，并发测速会占用大量带宽，建议配合 <code>total-speed-limit</code> 避免拉满出口。` });

    // 7. 节点筛选选项
    if (Array.isArray(nodeType) && nodeType.length > 0) suggests.push({ l: 'info', t: `已启用协议筛选：<code>${nodeType.join(' / ')}</code>，仅测试指定协议的节点。若需覆盖更多场景，可注释掉 <code>node-type</code>。` });
    if (ispCheck) suggests.push({ l: 'info', t: '<code>isp-check</code> 已开启，检测节点 ISP 类型（原生 / 住宅 / 机房），会增加少量检测耗时。' });
    if (dropBadCf) suggests.push({ l: 'info', t: '<code>drop-bad-cf-nodes: true</code> 已开启，无法访问 Cloudflare 的节点会被丢弃，可能导致可用节点明显减少。' });

    // 8. 节点数 & 地理分布
    const cfNum = parseFloat(String(qm.cf_consistent_ratio || '0')) || 0;
    if (cfNum > 80) suggests.push({ l: 'info', t: `CF 中转节点占比 ${qm.cf_consistent_ratio || cfNum + '%'} 偏高，CF 节点稳定性弱于独立 VPS，建议补充直连节点订阅源。` });
    if (geoCount >= 10) suggests.push({ l: 'good', t: `节点覆盖 ${geoCount} 个国家/地区，地理分布良好。` });
    else if (geoCount > 0 && geoCount < 5) suggests.push({ l: 'info', t: `节点仅覆盖 ${geoCount} 个地区，分布较集中，如有多地区需求建议增加订阅源。` });
    if (protoCount === 1) suggests.push({ l: 'info', t: '所有节点使用同一协议，多协议可提升不同网络环境下的兼容性。' });

    // 9. 版本更新
    if (!autoUpdate) suggests.push({ l: 'info', tab: 'advanced', t: '<code>update: false</code>，已关闭自动更新。仍会提示新版本，建议保持开启以获取最新修复。' });
    else if (!updateOnStart) suggests.push({ l: 'info', t: '<code>update-on-startup: false</code>，启动时不检查更新，版本可能滞后。' });

    // 10. 分享订阅
    if (!sharePassword) suggests.push({ l: 'info', tab: 'advanced', t: '未设置 <code>share-password</code>，订阅分享功能未启用。如需对外共享节点文件，设置后可通过 /sub/{password}/all.yaml 访问。' });

    // 11. 存储方式补充
    if (saveMethod !== 'local') suggests.push({ l: 'info', t: `存储方式为 <code>${esc(saveMethod)}</code>，请确认对应的凭证（gist-id / webdav 等）已正确配置，否则节点文件将无法保存。` });
    if (!suggests.length) suggests.push({ l: 'good', t: '配置状态良好，暂无优化建议。' });

    // svg 图标

    const SVG_OK = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;
    const SVG_WARN = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`;
    const SVG_INFO = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`;
    const getIcon = l => l === 'good' ? SVG_OK : l === 'warn' ? SVG_WARN : SVG_INFO;
    const LINK_ICON = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>`;
    const WARN_ICON = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="color:var(--warning)"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`;
    const ARROW_ICON = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>`;

    const deployHTML = deployCards.length
        ? `<div class="deploy-cards-col"><div class="section-title" style="margin:0 0 10px">待配置项</div>${deployCards.map(c => `<div class="deploy-card"><div class="deploy-card-title">${WARN_ICON}${c.title}</div><div class="deploy-card-desc">${c.desc}</div><div class="deploy-card-actions">${c.actions.map(a => `<a class="deploy-badge${a.primary ? ' primary' : ''}" href="${a.href}" target="_blank" rel="noopener">${LINK_ICON}${a.label}</a>`).join('')}${c.tab ? `<a class="deploy-badge" href="/admin#cfg-tab-${c.tab}" target="_blank" rel="noopener">${ARROW_ICON}配置入口</a>` : ''}</div></div>`).join('')}</div>`
        : `<div class="deploy-cards-col"><div class="suggest-item good" style="font-size:12px">${SVG_OK}<span>所有关键配置项已就绪。</span></div></div>`;

    const CFG_ADMIN_URL = '/admin';
    const renderSuggest = s => {
        const actionHtml = s.tab ? `<div><a class="suggest-action" href="${CFG_ADMIN_URL}#cfg-tab-${s.tab}" target="_blank" rel="noopener">${ARROW_ICON}去配置</a></div>` : '';
        return `<div class="suggest-item ${s.l}">${getIcon(s.l)}<div class="suggest-item-body"><div>${s.t}</div>${actionHtml}</div></div>`;
    };
    const suggestHTML = `<div><div class="section-title" style="margin:0 0 10px">配置建议</div><div class="suggest-list">${suggests.map(renderSuggest).join('')}</div></div>`;

    document.getElementById('cfganaContent').innerHTML = `
        <div class="section-title">本次检测参数</div>
        <div class="cfg-analysis-grid">
            <div class="cfg-card">
                <div class="cfg-card-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>运行结果</div>
                ${kvs.map(kv => `<div class="cfg-kv"><span class="cfg-k">${kv.k}</span><span class="cfg-v ${kv.cls || ''}">${kv.v}</span></div>`).join('')}
            </div>
            <div class="cfg-card">
                <div class="cfg-card-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/></svg>关键配置项</div>
                ${cfgKvs.map(kv => `<div class="cfg-kv"><span class="cfg-k">${kv.k}</span><span class="cfg-v ${kv.cls || ''}"${kv.title ? ` title="${esc(kv.title)}"` : ''}>${kv.v}</span></div>`).join('')}
            </div>
            <div class="cfg-card">
                <div class="cfg-card-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>并发 &amp; 速度参数</div>
                ${concKvs.map(kv => `<div class="cfg-kv"><span class="cfg-k">${kv.k}</span><span class="cfg-v ${kv.cls || ''}">${kv.v}</span></div>`).join('')}
            </div>
            <div class="cfg-card">
                <div class="cfg-card-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>节点分布概要</div>
                <div class="cfg-kv"><span class="cfg-k">覆盖地区</span><span class="cfg-v">${geoCount} 个</span></div>
                <div class="cfg-kv"><span class="cfg-k">协议种类</span><span class="cfg-v">${protoCount} 种</span></div>
                <div class="cfg-kv"><span class="cfg-k">CF 中转占比</span><span class="cfg-v">${qm.cf_consistent_ratio || '-'}</span></div>
                <div class="cfg-kv"><span class="cfg-k">独立 VPS 占比</span><span class="cfg-v">${total > 0 ? Math.round(vpsTotal / total * 100) + '%' : '-'}</span></div>
                <div class="cfg-kv"><span class="cfg-k">最优订阅节点数</span><span class="cfg-v ok">${sr[0] ? (sr[0].stats?.success || '-') + ' 个' : '-'}</span></div>
                <div class="cfg-kv"><span class="cfg-k">最优订阅通过率</span><span class="cfg-v ok">${sr[0] ? String(sr[0].stats?.rate || '-') : '-'}</span></div>
            </div>
        </div>
        <div class="cfg-bottom-grid">${deployHTML}${suggestHTML}</div>`;
}

function toggleBad() { const list = document.getElementById('badList'), chevron = document.getElementById('badChevron'); const open = list.style.display === 'none'; list.style.display = open ? '' : 'none'; chevron.classList.toggle('open', open); }
function esc(str) { return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }

async function writeClipboard(text) {
    if (navigator.clipboard && window.isSecureContext) { try { await navigator.clipboard.writeText(text); return true; } catch (e) { } }
    const ta = document.createElement('textarea');
    ta.value = text; ta.setAttribute('readonly', '');
    ta.style.cssText = 'position:absolute;top:' + (window.scrollY || document.documentElement.scrollTop) + 'px;left:0;width:1px;height:1px;padding:0;border:none;outline:none;box-shadow:none;background:transparent;opacity:0';
    document.body.appendChild(ta);
    const isIOS = /ipad|iphone/i.test(navigator.userAgent);
    if (isIOS) { const range = document.createRange(); range.selectNodeContents(ta); const sel = window.getSelection(); sel.removeAllRanges(); sel.addRange(range); ta.setSelectionRange(0, 999999); }
    else { ta.select(); }
    let ok = false; try { ok = document.execCommand('copy'); } catch (e) { }
    document.body.removeChild(ta); return ok;
}

if (document.getElementById('loginScene')) initPage();