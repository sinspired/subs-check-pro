package save

import (
	"os"
	"path/filepath"
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
	templateFile := filepath.Join(t.TempDir(), "ACL4SSR_Online_Full.yaml")
	template := "proxy-groups:\n  - name: test-group\n    type: select\n    proxies:\n      - DIRECT\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(templateFile, []byte(template), 0o644); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}
	config.GlobalConfig.MihomoOverwriteURL = templateFile

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

	var parsed map[string]any
	if err := yaml.Unmarshal(gotData, &parsed); err != nil {
		t.Fatalf("failed to parse saved yaml: %v", err)
	}

	proxiesAny, ok := parsed["proxies"].([]any)
	if !ok {
		t.Fatalf("expected proxies to be a list, got %#v", parsed["proxies"])
	}
	if len(proxiesAny) != 1 {
		t.Fatalf("expected 1 proxy in saved yaml, got %d", len(proxiesAny))
	}
	proxy0, ok := proxiesAny[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first proxy to be a mapping, got %#v", proxiesAny[0])
	}
	if proxy0["name"] != "test-node" {
		t.Fatalf("expected saved proxy name test-node, got %#v", proxy0["name"])
	}

	if _, ok := parsed["proxy-groups"]; !ok {
		t.Fatalf("expected saved yaml to keep proxy-groups")
	}
}

func TestMergeMihomoTemplateKeepsRules(t *testing.T) {
	template := []byte("proxy-groups:\n  - name: auto\n    type: select\n    proxies:\n      - DIRECT\nrules:\n  - MATCH,DIRECT\n")
	proxies := []map[string]any{
		{
			"name":   "node-a",
			"type":   "ss",
			"server": "1.1.1.1",
			"port":   443,
		},
	}

	data, err := mergeMihomoTemplate(template, proxies)
	if err != nil {
		t.Fatalf("mergeMihomoTemplate returned error: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse merged yaml: %v", err)
	}

	if _, ok := parsed["proxy-groups"]; !ok {
		t.Fatalf("expected merged yaml to contain proxy-groups")
	}
	if _, ok := parsed["rules"]; !ok {
		t.Fatalf("expected merged yaml to contain rules")
	}
}
