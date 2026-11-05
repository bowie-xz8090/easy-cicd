package identity

import (
	"errors"
	"regexp"
	"strings"
)

var phonePattern = regexp.MustCompile(`^1[0-9]{10}$`)

// Principal is established by the server, never decoded from a deployment request.
type Principal struct {
	Type  string `json:"-"`
	Value string `json:"-"`
	IP    string `json:"-"`
}

func NormalizePhone(value string) (string, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "+86")
	if !phonePattern.MatchString(value) {
		return "", errors.New("a valid mainland mobile number is required")
	}
	return value, nil
}

func (p Principal) Display() string {
	if p.Type == "ip" {
		return p.Value
	}
	if p.Type == "oidc" {
		phone, err := NormalizePhone(p.Value)
		if err == nil {
			return phone[:3] + "****" + phone[7:]
		}
	}
	return "—"
}
