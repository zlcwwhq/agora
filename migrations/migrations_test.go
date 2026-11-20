package migrations

import (
	"regexp"
	"strconv"
	"testing"
)

func TestSchemaSetsVersion(t *testing.T) {
	m := regexp.MustCompile(`(?m)^PRAGMA user_version = (\d+);$`).FindAllStringSubmatch(Schema, -1)
	if len(m) != 1 {
		t.Fatalf("schema.sql must set user_version exactly once, found %d", len(m))
	}
	if v, _ := strconv.Atoi(m[0][1]); v != Version {
		t.Fatalf("schema.sql sets user_version %d, Version is %d", v, Version)
	}
}
