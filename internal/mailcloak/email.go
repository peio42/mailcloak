package mailcloak

import "strings"

func domainFromEmail(email string) (string, bool) {
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at >= len(email)-1 {
		return "", false
	}
	return strings.ToLower(email[at+1:]), true
}
