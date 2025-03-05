package save

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
)

func TestGenUrls(t *testing.T) {
	tests := []struct {
		name    string
		input   []map[string]any
		want    string
		wantErr bool
	}{
		{
			name: "valid hysteria2 config",
			input: []map[string]any{
				{
					"type":     "hysteria2",
					"uuid":     "b82f14be-9225-48cb-963e-0350c86c31d3",
					"server":   "us2.interld123456789.com",
					"port":     32000,
					"name":     "美国hy2-2-联通电信",
					"insecure": 1,
					"sni":      "234224.1234567890spcloud.com",
					"mport":    "32000-33000",
				},
			},
			want:    "hysteria2://b82f14be-9225-48cb-963e-0350c86c31d3@us2.interld123456789.com:32000?insecure=1&mport=32000-33000&sni=234224.1234567890spcloud.com#美国hy2-2-联通电信\n",
			wantErr: false,
		},
		{
			name: "multiple configs",
			input: []map[string]any{
				{
					"type":     "hysteria2",
					"uuid":     "b82f14be-9225-48cb-963e-0350c86c31d3",
					"server":   "us2.interld123456789.com",
					"port":     32000,
					"name":     "美国hy2-2-联通电信",
					"insecure": 1,
					"sni":      "234224.1234567890spcloud.com",
				},
			},
			want:    "hysteria2://b82f14be-9225-48cb-963e-0350c86c31d3@us2.interld123456789.com:32000?insecure=1&sni=234224.1234567890spcloud.com#美国hy2-2-联通电信\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("Failed to marshal input: %v", err)
			}

			got, err := genUrls(data)

			if (err != nil) != tt.wantErr {
				t.Errorf("genUrls() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			parsedGot, _ := url.Parse(got.String())

			// 重建 URL 进行比较
			gotDecoded := fmt.Sprintf("%s://%s@%s%s?%s#%s",
				parsedGot.Scheme,
				parsedGot.User.String(),
				parsedGot.Host,
				parsedGot.Path,
				parsedGot.RawQuery,
				parsedGot.Fragment)
			if gotDecoded != tt.want {
				t.Errorf("genUrls() = %v, want %v", got, tt.want)
			}
		})
	}
}
