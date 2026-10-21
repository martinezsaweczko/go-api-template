package model

// BuildInfo is the build metadata injected at link time.
type BuildInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	AppName   string `json:"app_name"`
}

// VersionInfo is the version endpoint response.
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"buildTime"`
	AppName   string `json:"appName"`
}

// HTTPError is a generic JSON error response.
type HTTPError struct {
	Message string `json:"message"`
}
