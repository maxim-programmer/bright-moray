package handler

import (
	"regexp"
	"strings"
)

// slugRe — допустимый id категории и подкатегории: латиница, цифры и дефисы.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z",
	'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r",
	'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// slugify превращает название в id: «Мебель и интерьер» → «mebel-i-interer».
// Пустой результат (название из одних символов, которых нет в алфавите) заменяется на fallback.
func slugify(name, fallback string) string {
	var b strings.Builder
	dash := true // не начинаем с дефиса и не повторяем его
	for _, r := range strings.ToLower(name) {
		var part string
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			part = string(r)
		default:
			part = translit[r]
		}
		if part == "" {
			if _, isRu := translit[r]; isRu { // «ъ» и «ь» просто пропускаются
				continue
			}
			if !dash {
				b.WriteByte('-')
				dash = true
			}
			continue
		}
		b.WriteString(part)
		dash = false
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 48 { // запас под числовой суффикс при совпадении
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		return fallback
	}
	return s
}
