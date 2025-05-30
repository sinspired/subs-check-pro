package method

import (
	"testing"

	"github.com/beck-8/subs-check/config"
)

func TestUploadToMinio(t *testing.T) {
	config.GlobalConfig.MinioEndpoint = "127.0.0.1:9000"
	config.GlobalConfig.MinioAccessID = "123"
	config.GlobalConfig.MinioSecretKey = "123"
	config.GlobalConfig.MinioBucket = "public"
	config.GlobalConfig.MinioUseSSL = false
	type args struct {
		data     []byte
		filename string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "TEST MINIO",
			args: args{
				data:     []byte("test"),
				filename: "test.yaml",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UploadToMinio(tt.args.data, tt.args.filename); (err != nil) != tt.wantErr {
				t.Errorf("UploadToMinio() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
