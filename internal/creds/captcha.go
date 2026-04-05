package creds

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CaptchaErrorKind classifies captcha failures for the caller.
type CaptchaErrorKind int

const (
	CaptchaAutoFailed   CaptchaErrorKind = iota // Auto solver returned BOT/ERROR_LIMIT
	CaptchaManualNeeded                         // Auto failed, manual verification possible
	CaptchaUnsupported                          // Image captcha or unknown type
	CaptchaNetworkError                         // Network failure during captcha
)

// CaptchaError carries classification and redirect URI for manual fallback.
type CaptchaError struct {
	Kind        CaptchaErrorKind
	Message     string
	RedirectURI string // For manual flow — user opens this in browser
}

func (e *CaptchaError) Error() string { return e.Message }

type vkCaptchaError struct {
	ErrorCode      int
	ErrorMsg       string
	CaptchaSid     string
	RedirectURI    string
	SessionToken   string
	CaptchaTs      string
	CaptchaAttempt string
}

func parseVkCaptchaError(errData map[string]interface{}) *vkCaptchaError {
	codeFloat, _ := errData["error_code"].(float64)
	redirectURI, _ := errData["redirect_uri"].(string)
	errorMsg, _ := errData["error_msg"].(string)

	captchaSid, _ := errData["captcha_sid"].(string)
	if captchaSid == "" {
		if sidNum, ok := errData["captcha_sid"].(float64); ok {
			captchaSid = fmt.Sprintf("%.0f", sidNum)
		}
	}

	var sessionToken string
	if redirectURI != "" {
		if parsed, err := neturl.Parse(redirectURI); err == nil {
			sessionToken = parsed.Query().Get("session_token")
		}
	}

	var captchaTs string
	if tsFloat, ok := errData["captcha_ts"].(float64); ok {
		captchaTs = fmt.Sprintf("%.0f", tsFloat)
	} else if tsStr, ok := errData["captcha_ts"].(string); ok {
		captchaTs = tsStr
	}

	var captchaAttempt string
	if attFloat, ok := errData["captcha_attempt"].(float64); ok {
		captchaAttempt = fmt.Sprintf("%.0f", attFloat)
	} else if attStr, ok := errData["captcha_attempt"].(string); ok {
		captchaAttempt = attStr
	}

	return &vkCaptchaError{
		ErrorCode:      int(codeFloat),
		ErrorMsg:       errorMsg,
		CaptchaSid:     captchaSid,
		RedirectURI:    redirectURI,
		SessionToken:   sessionToken,
		CaptchaTs:      captchaTs,
		CaptchaAttempt: captchaAttempt,
	}
}

func solveVkCaptcha(ctx context.Context, ce *vkCaptchaError, transport http.RoundTripper, logf LogFunc) (string, error) {
	logf("Solving VK PoW captcha...")
	if ce.SessionToken == "" {
		return "", &CaptchaError{
			Kind:    CaptchaUnsupported,
			Message: "no session_token — image captcha not supported",
		}
	}

	powInput, difficulty, err := fetchPowInput(ctx, ce.RedirectURI, transport)
	if err != nil {
		return "", &CaptchaError{
			Kind:    CaptchaNetworkError,
			Message: fmt.Sprintf("fetch PoW: %s", err),
		}
	}

	logf(fmt.Sprintf("PoW difficulty=%d, computing...", difficulty))
	hash := solvePoW(powInput, difficulty)
	if hash == "" {
		return "", &CaptchaError{
			Kind:    CaptchaAutoFailed,
			Message: "PoW exhausted nonce space",
		}
	}

	successToken, err := callCaptchaNotRobot(ctx, ce.SessionToken, hash, transport)
	if err != nil {
		// BOT or ERROR_LIMIT — manual fallback possible
		return "", &CaptchaError{
			Kind:        CaptchaManualNeeded,
			Message:     fmt.Sprintf("captcha auto-solve failed: %s", err),
			RedirectURI: ce.RedirectURI,
		}
	}

	logf("VK captcha solved")
	return successToken, nil
}

func fetchPowInput(ctx context.Context, redirectURI string, transport http.RoundTripper) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", redirectURI, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", randomUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: 20 * time.Second, Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, err
	}
	html := string(body)

	powInputRe := regexp.MustCompile(`const\s+powInput\s*=\s*"([^"]+)"`)
	m := powInputRe.FindStringSubmatch(html)
	if len(m) < 2 {
		return "", 0, fmt.Errorf("powInput not found in captcha HTML")
	}

	diffRe := regexp.MustCompile(`startsWith\('0'\.repeat\((\d+)\)\)`)
	dm := diffRe.FindStringSubmatch(html)
	difficulty := 2
	if len(dm) >= 2 {
		if d, err := strconv.Atoi(dm[1]); err == nil {
			difficulty = d
		}
	}
	return m[1], difficulty, nil
}

func solvePoW(powInput string, difficulty int) string {
	target := strings.Repeat("0", difficulty)
	for nonce := 1; nonce <= 10_000_000; nonce++ {
		data := powInput + strconv.Itoa(nonce)
		hash := sha256.Sum256([]byte(data))
		hexHash := hex.EncodeToString(hash[:])
		if strings.HasPrefix(hexHash, target) {
			return hexHash
		}
	}
	return ""
}

func callCaptchaNotRobot(ctx context.Context, sessionToken, hash string, transport http.RoundTripper) (string, error) {
	bp := RandomProfile()
	vkReq := func(method, postData string) (map[string]interface{}, error) {
		reqURL := "https://api.vk.ru/method/" + method + "?v=5.131"
		req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(postData))
		if err != nil {
			return nil, err
		}
		bp.ApplyHeaders(req)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://vk.ru")
		req.Header.Set("Referer", "https://vk.ru/")

		client := &http.Client{Timeout: 20 * time.Second, Transport: transport}
		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = httpResp.Body.Close() }()

		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, err
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		return resp, nil
	}

	baseParams := fmt.Sprintf("session_token=%s&domain=vk.com&adFp=&access_token=", neturl.QueryEscape(sessionToken))

	// Step 1: settings (random delay to look human)
	if _, err := vkReq("captchaNotRobot.settings", baseParams); err != nil {
		return "", fmt.Errorf("settings: %w", err)
	}
	time.Sleep(time.Duration(300+rand.Intn(500)) * time.Millisecond)

	// Step 2: componentDone (randomize fingerprint)
	browserFp := fmt.Sprintf("%032x", rand.Int63()) + fmt.Sprintf("%032x", rand.Int63())
	browserFp = browserFp[:32]

	screens := [][2]int{{1920, 1080}, {1536, 864}, {1440, 900}, {1366, 768}, {2560, 1440}}
	screen := screens[rand.Intn(len(screens))]
	concurrency := []int{4, 8, 12, 16}[rand.Intn(4)]
	memory := []int{4, 8, 16}[rand.Intn(3)]

	deviceJSON := fmt.Sprintf(`{"screenWidth":%d,"screenHeight":%d,"screenAvailWidth":%d,"screenAvailHeight":%d,"innerWidth":%d,"innerHeight":%d,"devicePixelRatio":1,"language":"ru-RU","languages":["ru-RU","ru"],"webdriver":false,"hardwareConcurrency":%d,"deviceMemory":%d,"connectionEffectiveType":"4g","notificationsPermission":"default"}`,
		screen[0], screen[1], screen[0], screen[1]-48, screen[0], screen[1]-135, concurrency, memory)

	componentDoneData := baseParams + fmt.Sprintf("&browser_fp=%s&device=%s", browserFp, neturl.QueryEscape(deviceJSON))

	if _, err := vkReq("captchaNotRobot.componentDone", componentDoneData); err != nil {
		return "", fmt.Errorf("componentDone: %w", err)
	}
	time.Sleep(time.Duration(400+rand.Intn(800)) * time.Millisecond)

	// Step 3: check (randomize cursor path)
	baseX := 900 + rand.Intn(200)
	baseY := 450 + rand.Intn(100)
	cursorJSON := fmt.Sprintf(`[{"x":%d,"y":%d},{"x":%d,"y":%d},{"x":%d,"y":%d},{"x":%d,"y":%d},{"x":%d,"y":%d}]`,
		baseX, baseY, baseX-3-rand.Intn(5), baseY+8+rand.Intn(5),
		baseX-7-rand.Intn(5), baseY+16+rand.Intn(5), baseX-10-rand.Intn(3), baseY+22+rand.Intn(5),
		baseX-10-rand.Intn(3), baseY+22+rand.Intn(3))
	answer := base64.StdEncoding.EncodeToString([]byte("{}"))
	debugHash := sha256.Sum256([]byte(fmt.Sprintf("%d%s", time.Now().UnixNano(), browserFp)))
	debugInfo := hex.EncodeToString(debugHash[:])

	checkData := baseParams + fmt.Sprintf(
		"&accelerometer=%s&gyroscope=%s&motion=%s&cursor=%s&taps=%s&connectionRtt=%s&connectionDownlink=%s&browser_fp=%s&hash=%s&answer=%s&debug_info=%s",
		neturl.QueryEscape("[]"), neturl.QueryEscape("[]"), neturl.QueryEscape("[]"),
		neturl.QueryEscape(cursorJSON), neturl.QueryEscape("[]"), neturl.QueryEscape("[]"),
		neturl.QueryEscape("[9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5]"),
		browserFp, hash, answer, debugInfo,
	)

	checkResp, err := vkReq("captchaNotRobot.check", checkData)
	if err != nil {
		return "", fmt.Errorf("check: %w", err)
	}

	respObj, ok := checkResp["response"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid check response: %v", checkResp)
	}
	status, _ := respObj["status"].(string)
	if status != "OK" {
		return "", fmt.Errorf("check status: %s", status)
	}
	successToken, ok := respObj["success_token"].(string)
	if !ok || successToken == "" {
		return "", fmt.Errorf("success_token not found")
	}

	// End session (best-effort)
	_, _ = vkReq("captchaNotRobot.endSession", baseParams)

	return successToken, nil
}
