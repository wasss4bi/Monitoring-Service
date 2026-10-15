package service

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var moneyNumber = regexp.MustCompile("^[+-]?(?:[0-9]+|[0-9]{1,3}(?:[ \u00a0\u2007\u2009\u202f][0-9]{3})+)(?:[.,][0-9]{1,2})?$")

func ruleNumericValue(raw, format string) (float64, bool) {
	text := strings.TrimSpace(raw)
	if format == "money" {
		text = strings.TrimSpace(strings.Trim(text, "$\u20ac\u00a3\u00a5\u20bd"))
		if !moneyNumber.MatchString(text) {
			return 0, false
		}
		text = strings.Map(func(r rune) rune {
			switch r {
			case ' ', '\u00a0', '\u2007', '\u2009', '\u202f':
				return -1
			case ',':
				return '.'
			}
			return r
		}, text)
	}
	n, err := strconv.ParseFloat(text, 64)
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
