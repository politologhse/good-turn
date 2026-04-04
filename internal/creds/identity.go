package creds

import (
	"fmt"
	"math/rand"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 11.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 YaBrowser/24.1.0.0 Yowser/2.5 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
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

func randomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
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
