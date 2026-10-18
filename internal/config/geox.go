package config

// GeoXBaseURL is the jsDelivr mirror used for mihomo geodata downloads.
const GeoXBaseURL = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release"

// DefaultGeoXURL maps mihomo's geox-url keys to the mirror assets.
var DefaultGeoXURL = map[string]string{
	"geoip":   GeoXBaseURL + "/geoip.dat",
	"geosite": GeoXBaseURL + "/geosite.dat",
	"mmdb":    GeoXBaseURL + "/geoip.metadb",
	"asn":     GeoXBaseURL + "/GeoLite2-ASN.mmdb",
}

// ApplyGeoXDefaults sets the default geox-url mapping unless the configuration
// already provides a geox-url value.
func ApplyGeoXDefaults(cfg map[string]any) {
	if cfg == nil {
		return
	}
	if _, exists := cfg["geox-url"]; exists {
		return
	}
	urls := make(map[string]any, len(DefaultGeoXURL))
	for key, value := range DefaultGeoXURL {
		urls[key] = value
	}
	cfg["geox-url"] = urls
}
