package SoutuBot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"time"

	fs "github.com/Miuzarte/FlareSolverr-go"
)

const (
	MAIN_PAGE_URL = `https://soutubot.moe`
	API_URL       = `https://soutubot.moe/api/search`
)

type Client struct {
	FlareSolverrClient *fs.Client

	cache struct {
		userAgent string
		cookies   []*http.Cookie
		m         int64
	}
}

type HttpError struct {
	StatusCode int
	Url        string
	Body       string
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("http error %d: %s, %s", e.StatusCode, e.Url, e.Body)
}

func NewClient(fsClient *fs.Client) *Client {
	return &Client{
		FlareSolverrClient: fsClient,
	}
}

func (c *Client) Search(ctx context.Context, imgData []byte) (*Response, error) {
	return c.do(ctx, func() (*http.Request, error) {
		return c.buildRequest(ctx, imgData)
	})
}

func (c *Client) do(ctx context.Context, requestBuilder func() (*http.Request, error)) (*Response, error) {
	if c.cacheEmpty() {
		c.bypassCfAndGetM(ctx)
	}

	const bypassCfRetryTimes = 1
	numRetries := bypassCfRetryTimes
TRYAGAIN:
	req, err := requestBuilder()
	if err != nil {
		return nil, err
	}

	hResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer hResp.Body.Close()

	switch hResp.StatusCode {
	case http.StatusOK:
		break

	case http.StatusUnauthorized, http.StatusForbidden:
		// 尝试过 cf 重新拿 m
		_, _, _, e := c.bypassCfAndGetM(ctx)
		if e == nil {
			if numRetries > 0 {
				// 成功后重试一次
				numRetries--
				goto TRYAGAIN
			}
		}
		fallthrough
	default:
		const bodyTruncateLen = 1024
		body, _ := io.ReadAll(hResp.Body)
		if len(body) > bodyTruncateLen {
			body = body[:bodyTruncateLen]
		}
		return nil, &HttpError{
			StatusCode: hResp.StatusCode,
			Url:        req.URL.String(),
			Body:       string(body),
		}
	}

	resp := &Response{}
	err = json.NewDecoder(hResp.Body).Decode(resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// fsGet 完成后缓存 user agent 和 cookies
func (c *Client) fsGet(ctx context.Context, url string) (string, error) {
	if c.FlareSolverrClient == nil {
		return "", fmt.Errorf("FlareSolverrClient is not set")
	}
	resp, err := c.FlareSolverrClient.Get(ctx, url, map[string]any{
		fs.PARAM_MAX_TIMEOUT: 60000,
	})
	if err != nil {
		return "", err
	}
	if resp.Solution.Status != http.StatusOK {
		return "", fmt.Errorf("flaresolverr failed %d: %s, %s",
			resp.Solution.Status, resp.Message, resp.Solution.Response)
	}

	// cache user agent and cookies
	c.cache.userAgent = resp.Solution.UserAgent
	c.cache.cookies = resp.Solution.Cookies.ToHttpCookies()
	return resp.Solution.Response, nil
}

// bypassCfAndGetM 访问主页获取 cf challenge 凭证并更新 m 值
func (c *Client) bypassCfAndGetM(ctx context.Context) (ua string, cookies []*http.Cookie, m int64, err error) {
	body, err := c.fsGet(ctx, MAIN_PAGE_URL)
	if err != nil {
		return "", nil, 0, err
	}
	c.cache.m = bodyGetGlobalM(body)
	if c.cache.m <= 0 {
		return "", nil, 0, fmt.Errorf("failed to get global m: %d", m)
	}
	return c.cache.userAgent, c.cache.cookies, c.cache.m, nil
}

func (c *Client) cacheEmpty() bool {
	return c.cache.userAgent == "" || c.cache.cookies == nil || c.cache.m == 0
}

func bodyGetGlobalM(body string) (m int64) {
	const prefix = "m: "
	const suffix = ","
	prefixIndex := strings.Index(body, prefix)
	if prefixIndex == -1 {
		return -1
	}
	body = body[prefixIndex+len(prefix):]
	suffixIndex := strings.Index(body, suffix)
	if suffixIndex == -1 {
		return -2
	}
	mStr := body[:suffixIndex]
	m, err := strconv.ParseInt(mStr, 10, 64)
	if err != nil {
		return -3
	}
	return m
}

func (c *Client) buildRequest(ctx context.Context, imgData []byte) (*http.Request, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	err := w.SetBoundary(WEBKIT_BOUNDARY_PREFIX + randAsciiSuffix(WEBKIT_BOUNDARY_SUFFIX_LEN))
	if err != nil {
		return nil, err
	}

	fh := textproto.MIMEHeader{}
	fh.Set("Content-Disposition", `form-data; name="file"; filename="image"`)
	fh.Set("Content-Type", `application/octet-stream`)

	fp, err := w.CreatePart(fh)
	if err != nil {
		return nil, err
	}
	if _, err := fp.Write(imgData); err != nil {
		return nil, err
	}
	err = w.WriteField("factor", "1.2") // 好像都是 1.2
	if err != nil {
		return nil, err
	}
	err = w.Close()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, API_URL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://soutubot.moe")
	req.Header.Set("Referer", "https://soutubot.moe/")
	req.Header.Set("Dnt", "1")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", c.cache.userAgent)
	req.Header.Set("X-Api-Key", calcApiKey(len(c.cache.userAgent), c.cache.m))
	for _, c := range c.cache.cookies {
		req.AddCookie(c)
	}

	return req, nil
}

const (
	WEBKIT_BOUNDARY_PREFIX     = "----WebKitFormBoundary" // +"xb1PFceXlCoUBXX8"(16)
	WEBKIT_BOUNDARY_SUFFIX_LEN = 16
)

// randAsciiSuffix 生成长度为 n 的随机 ascii 字符串作为 boundary 的后缀
func randAsciiSuffix(n int) string {
	if n <= 0 {
		return ""
	}
	enc := base64.RawURLEncoding
	buf := make([]byte, enc.DecodedLen(n))
	rand.Read(buf)
	return enc.EncodeToString(buf)[:n]
}

func calcApiKey(uaLen int, m int64) string {
	// const mC = () => {
	//     const e = (Math.pow(Z().unix(), 2) + Math.pow(window.navigator.userAgent.length, 2) + window.GLOBAL.m).toString();
	//     return En.encode(e).split("").reverse().join("").replace(/=/g, "")
	// }

	ts := float64(time.Now().Unix())
	sum := math.Pow(ts, 2) + math.Pow(float64(uaLen), 2) + float64(m)
	es := strconv.FormatFloat(sum, 'g', -1, 64)
	b64 := []byte(base64.StdEncoding.EncodeToString([]byte(es)))
	slices.Reverse(b64)
	return strings.ReplaceAll(string(b64), "=", "")
}

const (
	MATCH_SIMILARITY_THRESHOLD = 45.0 // 最大匹配度低于45，结果可能不正确\n请自行判断，或更换严格模式/其他搜图引擎来搜索
	LOW_SIMILARITY_THRESHOLD   = 30.0 // 被隐藏
)

type Response struct {
	Data          []Item  `json:"data"`
	Id            string  `json:"id"`            // "2025102006392555" //  https://soutubot.moe/results/{.Id}
	Factor        float64 `json:"factor"`        // 1.2
	ImageUrl      string  `json:"imageUrl"`      // "https:\/\/img.76888268.xyz\/img\/8ed83259e082ce11285d13ce38718244.webp"
	SearchOption  string  `json:"searchOption"`  // "api 1.2 Liner 64 400x"
	ExecutionTime float64 `json:"executionTime"` // 耗时 (s)
}

type Item struct {
	Source          Source   `json:"source"` // "nhentai"|"ehentai"
	Page            int      `json:"page"`
	Title           string   `json:"title"`
	Language        Language `json:"language"`    // "cn"|"jp"
	PagePath        string   `json:"pagePath"`    // /g/480041/7
	SubjectPath     string   `json:"subjectPath"` // /g/480041
	PreviewImageUrl string   `json:"previewImageUrl"`
	Similarity      float64  `json:"similarity"` // 低匹配度阈值为 30
}

type Source string

func (s Source) Hosts() [2]string {
	if hosts, ok := sourceToHosts[string(s)]; ok {
		return hosts
	}
	return [2]string{string(s), string(s)}
}

var sourceToHosts = map[string][2]string{
	"nhentai": {"https://nhentai.net", "https://nhentai.xxx"},
	"ehentai": {"https://e-hentai.org", "https://exhentai.org"},
}

type Language string

func (l Language) Emoji() string {
	if emoji, ok := languageToEmoji[string(l)]; ok {
		return emoji
	}
	return string(l)
}

var languageToEmoji = map[string]string{
	"cn": "🇨🇳",
	"jp": "🇯🇵",
	"gb": "🇬🇧",
}
