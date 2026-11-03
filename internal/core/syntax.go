package core

import (
	"html"
	"strings"
	"unicode"
	"unicode/utf8"
)

func isASCIIPunctuation(code byte) bool {
	return code >= '!' && code <= '/' || code >= ':' && code <= '@' || code >= '[' && code <= '`' || code >= '{' && code <= '~'
}

func isASCIIAlphanumeric(code byte) bool {
	return code >= '0' && code <= '9' || code >= 'A' && code <= 'Z' || code >= 'a' && code <= 'z'
}

func isUnicodeWhitespace(code rune) bool {
	if code == EOF {
		return true
	}
	switch code {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return code >= '\u2000' && code <= '\u200a'
}

func isUnicodePunctuation(code rune) bool {
	return unicode.IsPunct(code) || unicode.IsSymbol(code)
}

func utf16Length(value string) int {
	length := 0
	for _, code := range value {
		length++
		if code > 0xffff {
			length++
		}
	}
	return length
}

func namedEntity(name string) bool {
	low, high := 0, len(htmlEntityNames)
	for low < high {
		middle := low + (high-low)/2
		if htmlEntityNames[middle] < name {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low < len(htmlEntityNames) && htmlEntityNames[low] == name
}

func scanCharacterReference(input string, start int) (value string, end int, ok bool) {
	if start >= len(input) || input[start] != '&' || start+2 >= len(input) {
		return "", start, false
	}
	index := start + 1
	if input[index] == '#' {
		index++
		base, maximum := 10, 7
		if index < len(input) && (input[index] == 'x' || input[index] == 'X') {
			base, maximum = 16, 6
			index++
		}
		digitStart, number := index, 0
		for index < len(input) && index-digitStart < maximum {
			digit := -1
			code := input[index]
			if code >= '0' && code <= '9' {
				digit = int(code - '0')
			} else if base == 16 && code >= 'a' && code <= 'f' {
				digit = int(code-'a') + 10
			} else if base == 16 && code >= 'A' && code <= 'F' {
				digit = int(code-'A') + 10
			}
			if digit < 0 {
				break
			}
			number = number*base + digit
			index++
		}
		if index == digitStart || index >= len(input) || input[index] != ';' {
			return "", start, false
		}
		if number < 9 || number == 11 || number > 13 && number < 32 || number > 126 && number < 160 || number >= 0xd800 && number <= 0xdfff || number >= 0xfdd0 && number <= 0xfdef || number&0xffff == 0xfffe || number&0xffff == 0xffff || number > utf8.MaxRune {
			number = int(utf8.RuneError)
		}
		return string(rune(number)), index + 1, true
	}
	if input[index] < 'A' || input[index] > 'Z' && input[index] < 'a' || input[index] > 'z' {
		return "", start, false
	}
	for index < len(input) && index-start-1 < 31 && isASCIIAlphanumeric(input[index]) {
		index++
	}
	if index >= len(input) || input[index] != ';' || !namedEntity(input[start+1:index]) {
		return "", start, false
	}
	return html.UnescapeString(input[start : index+1]), index + 1, true
}

func decodeMarkdown(input string) string { return decodeMarkdownWith(input, true, true) }

func (s *parseState) decodeString(input string) string {
	return decodeMarkdownWith(input, s.enabled("characterEscape"), s.enabled("characterReference"))
}

func decodeMarkdownWith(input string, escapes, references bool) string {
	var builder strings.Builder
	start := 0
	changed := false
	for index := 0; index < len(input); index++ {
		if escapes && input[index] == '\\' && index+1 < len(input) && isASCIIPunctuation(input[index+1]) {
			builder.WriteString(input[start:index])
			builder.WriteByte(input[index+1])
			index++
			start, changed = index+1, true
		} else if references && input[index] == '&' {
			if value, end, ok := scanCharacterReference(input, index); ok {
				builder.WriteString(input[start:index])
				builder.WriteString(value)
				index = end - 1
				start, changed = end, true
			}
		}
	}
	if !changed {
		return input
	}
	builder.WriteString(input[start:])
	return builder.String()
}

func canonicalRune(code rune) string {
	low, high := 0, len(caseFoldData)
	for low < high {
		middle := low + (high-low)/2
		if caseFoldData[middle].code < code {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low < len(caseFoldData) && caseFoldData[low].code == code {
		return caseFoldData[low].value
	}
	return ""
}

func normalizeIdentifier(input string) string {
	var builder strings.Builder
	pendingSpace := false
	started := false
	for _, code := range input {
		if code == ' ' || code == '\t' || code == '\r' || code == '\n' {
			pendingSpace = started
			continue
		}
		if pendingSpace {
			builder.WriteByte(' ')
			pendingSpace = false
		}
		started = true
		if code >= 'a' && code <= 'z' {
			builder.WriteByte(byte(code - 'a' + 'A'))
		} else if code > 127 {
			if folded := canonicalRune(code); folded != "" {
				builder.WriteString(folded)
			} else {
				builder.WriteRune(code)
			}
		} else {
			builder.WriteByte(byte(code))
		}
	}
	return builder.String()
}

func normalizeURI(input string) string {
	const hexadecimal = "0123456789ABCDEF"
	var builder strings.Builder
	start, changed := 0, false
	for index := 0; index < len(input); {
		code := input[index]
		if code == '%' && index+2 < len(input) && isASCIIAlphanumeric(input[index+1]) && isASCIIAlphanumeric(input[index+2]) {
			index += 3
			continue
		}
		allowed := code == '!' || code == '#' || code == '$' || code >= '&' && code <= ';' || code == '=' || code >= '?' && code <= 'Z' || code == '_' || code >= 'a' && code <= 'z' || code == '~'
		if code < utf8.RuneSelf && allowed {
			index++
			continue
		}
		builder.WriteString(input[start:index])
		if code < utf8.RuneSelf {
			builder.WriteByte('%')
			builder.WriteByte(hexadecimal[code>>4])
			builder.WriteByte(hexadecimal[code&15])
			index++
		} else {
			r, size := utf8.DecodeRuneInString(input[index:])
			if r == utf8.RuneError && size == 1 {
				builder.WriteString("%EF%BF%BD")
			} else {
				for _, b := range []byte(input[index : index+size]) {
					builder.WriteByte('%')
					builder.WriteByte(hexadecimal[b>>4])
					builder.WriteByte(hexadecimal[b&15])
				}
			}
			index += size
		}
		start, changed = index, true
	}
	if !changed {
		return input
	}
	builder.WriteString(input[start:])
	return builder.String()
}
