package format

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxWords — числа до триллионов включительно (меньше 10^15).
var maxWords = new(big.Int).Exp(big.NewInt(10), big.NewInt(15), nil)

var errNotInteger = errors.New("not an integer")

// Words — целое число словами: "шесть миллионов" (ru), "алты миллион" (kk).
func Words(v any, locale string) (string, error) {
	d, ok, err := Decimal(v)
	if err != nil || !ok {
		return "", err
	}
	if !d.IsInt() {
		return "", fmt.Errorf("%w: %s", errNotInteger, d.FloatString(2))
	}
	return intWords(d.Num(), locale)
}

// MoneyWords — сумма словами с тиынами цифрами: "шесть миллионов тенге 00 тиын",
// "алты миллион теңге 00 тиын". Округление до тиына — как в Money.
func MoneyWords(v any, locale string) (string, error) {
	d, ok, err := Decimal(v)
	if err != nil || !ok {
		return "", err
	}

	intPart, frac, _ := strings.Cut(strings.TrimPrefix(d.FloatString(2), "-"), ".")
	n, _ := new(big.Int).SetString(intPart, 10)
	if d.Sign() < 0 {
		n.Neg(n)
	}

	words, err := intWords(n, locale)
	if err != nil {
		return "", err
	}
	switch normalizeLocale(locale) {
	case "ru":
		return words + " тенге " + frac + " тиын", nil
	default:
		return words + " теңге " + frac + " тиын", nil
	}
}

// CapFirst делает первую букву заглавной: "шесть миллионов" → "Шесть миллионов".
func CapFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func intWords(n *big.Int, locale string) (string, error) {
	abs := new(big.Int).Abs(n)
	if abs.Cmp(maxWords) >= 0 {
		return "", fmt.Errorf("number %s is too large for words", n)
	}

	var words func(uint64) string
	switch normalizeLocale(locale) {
	case "ru":
		words = wordsRu
	case "kk":
		words = wordsKk
	default:
		return "", fmt.Errorf("unsupported locale %q: use ru or kk", locale)
	}

	s := words(abs.Uint64())
	if n.Sign() < 0 {
		s = "минус " + s
	}
	return s, nil
}

// groups разбивает число на тройки цифр, начиная с младших: 6000123 → [123 0 6].
func groups(n uint64) []int {
	var out []int
	for n > 0 {
		out = append(out, int(n%1000))
		n /= 1000
	}
	return out
}

var (
	ruUnitsMale   = [10]string{"", "один", "два", "три", "четыре", "пять", "шесть", "семь", "восемь", "девять"}
	ruUnitsFemale = [10]string{"", "одна", "две", "три", "четыре", "пять", "шесть", "семь", "восемь", "девять"}
	ruTeens       = [10]string{"десять", "одиннадцать", "двенадцать", "тринадцать", "четырнадцать",
		"пятнадцать", "шестнадцать", "семнадцать", "восемнадцать", "девятнадцать"}
	ruTens     = [10]string{"", "", "двадцать", "тридцать", "сорок", "пятьдесят", "шестьдесят", "семьдесят", "восемьдесят", "девяносто"}
	ruHundreds = [10]string{"", "сто", "двести", "триста", "четыреста", "пятьсот", "шестьсот", "семьсот", "восемьсот", "девятьсот"}

	// ruScales — названия разрядов в формах для 1, 2–4 и 5+; тысяча — женского рода.
	ruScales = []struct {
		forms  [3]string
		female bool
	}{
		{},
		{forms: [3]string{"тысяча", "тысячи", "тысяч"}, female: true},
		{forms: [3]string{"миллион", "миллиона", "миллионов"}},
		{forms: [3]string{"миллиард", "миллиарда", "миллиардов"}},
		{forms: [3]string{"триллион", "триллиона", "триллионов"}},
	}
)

func wordsRu(n uint64) string {
	if n == 0 {
		return "ноль"
	}

	var parts []string
	g := groups(n)
	for i := len(g) - 1; i >= 0; i-- {
		if g[i] == 0 {
			continue
		}
		parts = append(parts, tripleRu(g[i], ruScales[i].female)...)
		if i > 0 {
			parts = append(parts, ruScales[i].forms[ruPlural(g[i])])
		}
	}
	return strings.Join(parts, " ")
}

func tripleRu(n int, female bool) []string {
	var out []string
	if h := n / 100; h > 0 {
		out = append(out, ruHundreds[h])
	}
	switch t := n % 100; {
	case t >= 10 && t < 20:
		out = append(out, ruTeens[t-10])
	default:
		if t/10 > 0 {
			out = append(out, ruTens[t/10])
		}
		if u := t % 10; u > 0 {
			if female {
				out = append(out, ruUnitsFemale[u])
			} else {
				out = append(out, ruUnitsMale[u])
			}
		}
	}
	return out
}

// ruPlural возвращает индекс формы слова после числа n: 0 — «тысяча», 1 — «тысячи», 2 — «тысяч».
func ruPlural(n int) int {
	switch t := n % 100; {
	case t >= 11 && t <= 14:
		return 2
	case t%10 == 1:
		return 0
	case t%10 >= 2 && t%10 <= 4:
		return 1
	}
	return 2
}

var (
	kkUnits  = [10]string{"", "бір", "екі", "үш", "төрт", "бес", "алты", "жеті", "сегіз", "тоғыз"}
	kkTens   = [10]string{"", "он", "жиырма", "отыз", "қырық", "елу", "алпыс", "жетпіс", "сексен", "тоқсан"}
	kkScales = []string{"", "мың", "миллион", "миллиард", "триллион"}
)

// wordsKk — в казахском существительное после числа не меняется: «алты миллион», «екі мың».
func wordsKk(n uint64) string {
	if n == 0 {
		return "нөл"
	}

	var parts []string
	g := groups(n)
	for i := len(g) - 1; i >= 0; i-- {
		if g[i] == 0 {
			continue
		}
		parts = append(parts, tripleKk(g[i])...)
		if i > 0 {
			parts = append(parts, kkScales[i])
		}
	}
	return strings.Join(parts, " ")
}

// tripleKk — сотня без «бір»: «жүз», «жүз елу»; «бір» перед мың и выше ставится всегда.
func tripleKk(n int) []string {
	var out []string
	switch h := n / 100; h {
	case 0:
	case 1:
		out = append(out, "жүз")
	default:
		out = append(out, kkUnits[h], "жүз")
	}
	if t := n % 100 / 10; t > 0 {
		out = append(out, kkTens[t])
	}
	if u := n % 10; u > 0 {
		out = append(out, kkUnits[u])
	}
	return out
}
