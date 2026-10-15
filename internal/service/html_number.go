package service

import (
	"regexp"
	"strings"
	"unicode"
)

var htmlNumberSyntax = regexp.MustCompile(`^[+-]?[0-9][0-9 ,.]*$`)
var htmlSpaceGroups = regexp.MustCompile(`^[0-9]{1,3}( [0-9]{3})+(?:[,.][0-9]+)?$`)
var htmlCommaGroups = regexp.MustCompile(`^[0-9]{1,3}(?:,[0-9]{3})+\.[0-9]+$`)
var htmlDotGroups = regexp.MustCompile(`^[0-9]{1,3}(?:\.[0-9]{3})+,[0-9]+$`)
var htmlWholeGroups = regexp.MustCompile(`^[0-9]{1,3}(?: [0-9]{3})*$`)

// Keep the existing browser parser's ambiguity checks during extraction migration.
func htmlNumericValue(raw string) (float64, bool) {
	trim := func(s string) string {
		return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' })
	}
	value := trim(raw)
	value = strings.NewReplacer("\u00a0", " ", "\u2007", " ", "\u202f", " ").Replace(value)
	if len(value) > 0 && strings.ContainsRune("€$£¥₽", []rune(value)[0]) {
		value = trim(string([]rune(value)[1:]))
	}
	runes := []rune(value)
	if len(runes) > 0 && strings.ContainsRune("€$£¥₽", runes[len(runes)-1]) {
		value = trim(string(runes[:len(runes)-1]))
	}
	if !htmlNumberSyntax.MatchString(value) {
		return 0, false
	}
	sign := ""
	if value[0] == '+' || value[0] == '-' {
		sign = value[:1]
		value = value[1:]
	}
	dots, commas, spaces := strings.Count(value, "."), strings.Count(value, ","), strings.Count(value, " ")
	if dots > 1 || commas > 1 || spaces > 0 && !htmlSpaceGroups.MatchString(value) {
		return 0, false
	}
	if dots == 1 && commas == 1 {
		if !htmlCommaGroups.MatchString(value) && !htmlDotGroups.MatchString(value) {
			return 0, false
		}
		if strings.LastIndex(value, ".") > strings.LastIndex(value, ",") {
			value = strings.ReplaceAll(value, ",", "")
		} else {
			value = strings.ReplaceAll(value, ".", "")
			value = strings.ReplaceAll(value, ",", ".")
		}
	} else if dots == 1 || commas == 1 {
		separator := "."
		if commas == 1 {
			separator = ","
		}
		whole, fraction, _ := strings.Cut(value, separator)
		if len(fraction) == 3 && len(whole) <= 3 {
			return 0, false
		}
		if len(fraction) == 3 && htmlWholeGroups.MatchString(whole) {
			value = strings.ReplaceAll(whole, " ", "") + fraction
		} else {
			value = strings.ReplaceAll(whole, " ", "") + "." + fraction
		}
	} else {
		value = strings.ReplaceAll(value, " ", "")
	}
	return ruleNumericValue(sign+value, "")
}
