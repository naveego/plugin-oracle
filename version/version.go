package version

import "github.com/coreos/go-semver/semver"

var (
	// VersionMajor is for an API incompatible changes.
	VersionMajor int64 = 2
	// VersionMinor is for functionality in a backwards-compatible manner.
	VersionMinor int64 = 1
	// VersionPatch is for backwards-compatible bug fixes.
	VersionPatch int64 = 10
	// VersionPre indicates prerelease.
	VersionPre string = "beta"
	// VersionDev indicates development branch. Releases will be empty string.
	VersionDev string
)

// Version is the specification version that the package types support.
var Version = semver.Version{
	Major:      VersionMajor,
	Minor:      VersionMinor,
	Patch:      VersionPatch,
	PreRelease: semver.PreRelease(VersionPre),
	Metadata:   VersionDev,
}
