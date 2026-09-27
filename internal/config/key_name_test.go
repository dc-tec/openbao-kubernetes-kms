package config

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestTransitKeyNamesMatchOpenBaoMetadataPaths(t *testing.T) {
	// OpenBao v2.6.0 sdk/framework/path.go GenericNameRegex, with full anchoring.
	upstream := regexp.MustCompile(`^\w(([\w-.]+)?\w)?$`)
	var schema struct {
		Properties struct {
			Transit struct {
				Properties struct {
					KeyName struct{ Pattern string } `json:"keyName"`
				} `json:"properties"`
			} `json:"transit"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(SchemaJSON(), &schema); err != nil {
		t.Fatal(err)
	}
	schemaPattern := regexp.MustCompile(schema.Properties.Transit.Properties.KeyName.Pattern)
	for _, name := range []string{
		"k", "0", "_", "k8s-workload-a-etcd", "key.v2", "_key_", "A..--_B", "0123",
		"", ".", "..", "-", ".key", "key.", "-key", "key-", "key name", "key/name", "key%2Fname",
		"key?name", "key#name", "key*", "key+", "clé", " key", "key\n", "key\x00",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := loadValidConfig(t)
			cfg.Transit.KeyName = name
			original := cfg.Transit.KeyName
			err := Validate(cfg, ValidationOptions{})
			want := upstream.MatchString(name)
			if (err == nil) != want || schemaPattern.MatchString(name) != want {
				t.Fatalf("config/schema disagree with OpenBao for %q: %v", name, err)
			}
			if err != nil && !strings.Contains(err.Error(), "transit.keyName") {
				t.Fatalf("key-name error lacks its field: %v", err)
			}
			if cfg.Transit.KeyName != original {
				t.Fatal("validation rewrote an identity-bearing value")
			}
		})
	}
}
