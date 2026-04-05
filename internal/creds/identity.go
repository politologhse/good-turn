package creds

import (
	"fmt"
	"math/rand"
	"net/http"
)

// BrowserProfile pairs User-Agent with matching Client Hints headers.
// VK cross-references these for anti-bot detection.
type BrowserProfile struct {
	UserAgent   string
	SecCHUA     string
	SecPlatform string
	SecMobile   string
}

var browserProfiles = []BrowserProfile{
	{
		UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		SecCHUA:     `"Google Chrome";v="146", "Chromium";v="146", "Not.A/Brand";v="24"`,
		SecPlatform: `"Windows"`,
		SecMobile:   "?0",
	},
	{
		UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
		SecCHUA:     `"Google Chrome";v="145", "Chromium";v="145", "Not.A/Brand";v="24"`,
		SecPlatform: `"Windows"`,
		SecMobile:   "?0",
	},
	{
		UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
		SecCHUA:     `"Microsoft Edge";v="146", "Chromium";v="146", "Not.A/Brand";v="24"`,
		SecPlatform: `"Windows"`,
		SecMobile:   "?0",
	},
	{
		UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		SecCHUA:     `"Google Chrome";v="146", "Chromium";v="146", "Not.A/Brand";v="24"`,
		SecPlatform: `"macOS"`,
		SecMobile:   "?0",
	},
	{
		UserAgent:   "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		SecCHUA:     `"Google Chrome";v="146", "Chromium";v="146", "Not.A/Brand";v="24"`,
		SecPlatform: `"Linux"`,
		SecMobile:   "?0",
	},
	{
		UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 YaBrowser/24.1.0.0 Yowser/2.5 Safari/537.36",
		SecCHUA:     `"Yandex";v="24", "Chromium";v="144", "Not.A/Brand";v="24"`,
		SecPlatform: `"Windows"`,
		SecMobile:   "?0",
	},
}

// RandomProfile returns a random browser profile.
func RandomProfile() BrowserProfile {
	return browserProfiles[rand.Intn(len(browserProfiles))]
}

// ApplyHeaders sets all browser identity headers on an HTTP request.
func (bp BrowserProfile) ApplyHeaders(req *http.Request) {
	req.Header.Set("User-Agent", bp.UserAgent)
	req.Header.Set("Sec-CH-UA", bp.SecCHUA)
	req.Header.Set("Sec-CH-UA-Mobile", bp.SecMobile)
	req.Header.Set("Sec-CH-UA-Platform", bp.SecPlatform)
}

var firstNames = []string{
	"Александр", "Дмитрий", "Максим", "Сергей", "Андрей", "Алексей", "Артём", "Илья",
	"Кирилл", "Михаил", "Никита", "Матвей", "Роман", "Егор", "Арсений", "Иван",
	"Анна", "Мария", "Елена", "Дарья", "Анастасия", "Екатерина", "Виктория", "Ольга",
}

var lastNames = []string{
	"Иванов", "Смирнов", "Кузнецов", "Попов", "Васильев", "Петров", "Соколов", "Михайлов",
	"Новиков", "Федоров", "Морозов", "Волков", "Алексеев", "Лебедев", "Семенов", "Егоров",
}

func randomName() string {
	fn := firstNames[rand.Intn(len(firstNames))]
	if rand.Float32() < 0.3 {
		return fn
	}
	ln := lastNames[rand.Intn(len(lastNames))]
	lastChar := fn[len(fn)-2:] // 2 bytes for cyrillic
	if lastChar == "а" || lastChar == "я" {
		return fmt.Sprintf("%s %sа", fn, ln)
	}
	return fmt.Sprintf("%s %s", fn, ln)
}

// randomUserAgent returns a random user-agent string (for backward compat).
func randomUserAgent() string {
	return RandomProfile().UserAgent
}
