package assets

import (
	"embed"
)

//go:embed backend.version
var EmbeddedSubStoreBackendVer []byte

//go:embed sub-store.bundle.js
var EmbeddedSubStoreBackend []byte

//go:embed frontend
var EmbeddedSubStoreFrontend embed.FS

//go:embed Shadowrocket-Rules-CDN.conf
var EmbeddedShadowrocketConfig []byte

//go:embed ACL4SSR_Online_Full.yaml
var EmbeddedOverrideYamlACL4SSR []byte

//go:embed Mihomo-Rules-CDN.yaml
var EmbeddedOverrideYamlSinspiredRulesCDN []byte

//go:embed Mihomo-Rules-Lite-CDN.yaml
var EmbeddedOverrideYamlSinspiredRulesLiteCDN []byte

//go:embed scp-logo.svg
var EmbeddedSubsCheckProLogo []byte

//go:embed scp-app.svg
var EmbeddedScpAppLogo []byte

//go:embed sing-box.svg
var EmbeddedSingBoxLogo []byte
