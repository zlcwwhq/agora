package migrations

import (
	"regexp"
	"strconv"
	"testing"
)

func TestIndexSetsIndexVersion(t *testing.T) {
	m := regexp.MustCompile(`(?m)^PRAGMA user_version = (\d+);$`).FindAllStringSubmatch(Index, -1)
	if len(m) != 1 {
		t.Fatalf("index.sql must set user_version exactly once, found %d", len(m))
	}
	if v, _ := strconv.Atoi(m[0][1]); v != IndexVersion {
		t.Fatalf("index.sql sets user_version %d, IndexVersion is %d", v, IndexVersion)
	}
}
