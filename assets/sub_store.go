package assets

import (
	"embed"
)

//go:embed sub-store.bundle.js
var EmbeddedSubStoreBackend []byte

//go:embed frontend
var EmbeddedSubStoreFrontend embed.FS

//go:embed ACL4SSR_Online_Full.yaml
var EmbeddedOverrideYamlACL4SSR []byte

//go:embed Sinspired_Rules_CDN.yaml
var EmbeddedOverrideYamlSinspiredRulesCDN []byte
