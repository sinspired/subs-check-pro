package platform

import (
	"io"
	"net/http"
	"regexp"

	"github.com/biter777/countries"
)

// GeminiAccess Gemini 地区访问状态
type GeminiAccess uint8

const (
	AccessNormal  GeminiAccess = iota // 正常可访问
	AccessBlocked                     // 封锁名单内
	AccessSuspect                     // 封锁名单内但功能标记异常，可能已解封
)

// GeminiStatus Gemini 检测结果
type GeminiStatus struct {
	Region string // ISO 3166-1 alpha-2，空 = 不可访问
	IsEU   bool   // 受欧盟法规约束
	Access GeminiAccess
}

var (
	// viewer session 策略层: [1,null,null,<id>,<num>,"<ALPHA3>","<lang>",...]
	reRegion = regexp.MustCompile(`\[1,null,null,\d+,\d+,\\?"([A-Z]{3})\\?"`)
	// bw5YWe: 货币政策
	// reCurrency = regexp.MustCompile(`\[45615224,null,null,null,\\?"([^"\\]+)\\?",null,\\?"bw5YWe\\?"\]`)
	// 非 EU/封锁地区的完整功能标记；若封锁地区此二者为 true，封锁状态可能已变化
	reEdHLke = regexp.MustCompile(`\[45700351,null,(true|false),null,null,null,\\?"edHLke\\?"\]`)
	reXgpeRd = regexp.MustCompile(`\[45631641,null,(true|false),null,null,null,\\?"XgpeRd\\?"\]`)
)

// euMembers 受欧盟法规（GDPR / EU AI Act）约束的地区（ISO 3166-1 alpha-2）
// 包含全部 27 个 EU 成员国 + EEA（挪威、冰岛、列支敦士登）
var euMembers = map[string]bool{
	// EU 27
	"AT": true, "BE": true, "BG": true, "CY": true, "CZ": true,
	"DE": true, "DK": true, "EE": true, "ES": true, "FI": true,
	"FR": true, "GR": true, "HR": true, "HU": true, "IE": true,
	"IT": true, "LT": true, "LU": true, "LV": true, "MT": true,
	"NL": true, "PL": true, "PT": true, "RO": true, "SE": true,
	"SI": true, "SK": true,
	// EEA
	"NO": true, "IS": true, "LI": true,
}

// blockedRegions Gemini 不运营的地区（ISO 3166-1 alpha-3）
// Google 于 2026 年 3 月 16-17 日左右 开始逐步向香港所有用户开放 Gemini Web 应用，随后会开放移动 App。
var blockedRegions = map[string]bool{
	"CHN": true, "RUS": true, "BLR": true,
	"CUB": true, "IRN": true, "PRK": true,
	"SYR": true, "HKG": false, "MAC": false,
}

// https://github.com/clash-verge-rev/clash-verge-rev/blob/main/src-tauri/src/cmd/media_unlock_checker/gemini.rs

// CheckGemini 检测 Gemini 访问状态，Region 为空表示不可访问或解析失败
func CheckGemini(client *http.Client) (GeminiStatus, error) {
	req, err := http.NewRequest("GET", "https://gemini.google.com/", nil)
	if err != nil {
		return GeminiStatus{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return GeminiStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return GeminiStatus{}, nil
	}

	limitReader := io.LimitReader(resp.Body, 1024*1024)

	body, err := io.ReadAll(limitReader)
	if err != nil && err != io.EOF {
		return GeminiStatus{}, err
	}

	m := reRegion.FindSubmatch(body)
	if m == nil {
		return GeminiStatus{}, nil
	}

	a3 := string(m[1])
	region := toAlpha2(a3)

	// 直接由账户/IP 归属地区判断，不依赖货币标记
	isEU := euMembers[region]

	if !blockedRegions[a3] {
		return GeminiStatus{Region: region, IsEU: isEU, Access: AccessNormal}, nil
	}

	// 封锁名单内，交叉验证功能标记是否矛盾
	access := AccessBlocked
	if matchTrue(reEdHLke, body) && matchTrue(reXgpeRd, body) {
		access = AccessSuspect
	}
	return GeminiStatus{Region: region, IsEU: isEU, Access: access}, nil
}

func toAlpha2(a3 string) string {
	c := countries.ByName(a3)
	if c == countries.Unknown {
		return a3 // 未知时原样返回
	}
	return c.Alpha2()
}

func matchTrue(re *regexp.Regexp, body []byte) bool {
	m := re.FindSubmatch(body)
	return m != nil && string(m[1]) == "true"
}
