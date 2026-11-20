package migrations

import (
	"regexp"
	"strconv"
	"testing"
)

var setsVersion = regexp.MustCompile(`(?m)^PRAGMA user_version = (\d+);$`)

func TestSchemaSetsVersion(t *testing.T) {
	m := setsVersion.FindAllStringSubmatch(Schema, -1)
	if len(m) != 1 {
		t.Fatalf("schema.sql must set user_version exactly once, found %d", len(m))
	}
	if v, _ := strconv.Atoi(m[0][1]); v != Version {
		t.Fatalf("schema.sql sets user_version %d, Version is %d", v, Version)
	}
}

func TestUpgradesReachVersion(t *testing.T) {
	if len(Upgrades) != Version-2 {
		t.Fatalf("there are %d upgrades, want one for each version from 2 to %d", len(Upgrades), Version-1)
	}
	for from := 2; from < Version; from++ {
		up, ok := Upgrades[from]
		if !ok {
			t.Fatalf("no upgrade from version %d", from)
		}
		m := setsVersion.FindAllStringSubmatch(up, -1)
		if len(m) != 1 {
			t.Fatalf("the upgrade from version %d must set user_version exactly once, found %d", from, len(m))
		}
		if v, _ := strconv.Atoi(m[0][1]); v != from+1 {
			t.Fatalf("the upgrade from version %d sets user_version %d, want %d", from, v, from+1)
		}
	}
}
