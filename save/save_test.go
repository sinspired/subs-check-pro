package save

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/sinspired/subs-check-pro/config"
)

func TestSaveCategoryMihomoFallsBackWithoutSubStore(t *testing.T) {
	original := *config.GlobalConfig
	t.Cleanup(func() {
		*config.GlobalConfig = original
	})

	config.GlobalConfig.SubStorePort = ""

	proxies := []map[string]any{
		{
			"name":     "test-node",
			"type":     "ss",
			"server":   "1.1.1.1",
			"port":     443,
			"cipher":   "aes-128-gcm",
			"password": "test",
		},
	}

	var gotFile string
	var gotData []byte
	saver := &ConfigSaver{
		saveMethod: func(data []byte, filename string) error {
			gotFile = filename
			gotData = append([]byte(nil), data...)
			return nil
		},
	}

	if err := saver.saveCategory(ProxyCategory{Name: "mihomo.yaml", Proxies: proxies}); err != nil {
		t.Fatalf("saveCategory returned error: %v", err)
	}

	if gotFile != "mihomo.yaml" {
		t.Fatalf("expected save to mihomo.yaml, got %q", gotFile)
	}

	var parsed map[string][]map[string]any
	if err := yaml.Unmarshal(gotData, &parsed); err != nil {
		t.Fatalf("failed to parse saved yaml: %v", err)
	}

	if len(parsed["proxies"]) != 1 {
		t.Fatalf("expected 1 proxy in saved yaml, got %d", len(parsed["proxies"]))
	}

	if parsed["proxies"][0]["name"] != "test-node" {
		t.Fatalf("expected saved proxy name test-node, got %#v", parsed["proxies"][0]["name"])
	}
}
