package assets

import (
	_ "embed"
)

//go:embed sub-store.bundle.js.zst
var EmbeddedSubStore []byte

//go:embed ACL4SSR_Online_Full.yaml.zst
var EmbeddedOverrideYaml []byte
