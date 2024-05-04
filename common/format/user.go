package format

import (
	"fmt"
	"strings"
)

func UserTag(tag string, uuid string) string {
	return fmt.Sprintf("%s|%s", tag, uuid)
}

func ParseUserTag(userTag string) (tag string, uuid string, err error) {
	parts := strings.Split(userTag, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid user tag format: %s", userTag)
	}
	return parts[0], parts[1], nil
}
