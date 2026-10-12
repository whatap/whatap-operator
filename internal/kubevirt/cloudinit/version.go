package cloudinit

import "regexp"

// DefaultAgentVersion pins the first-boot tar release, never a moving latest URL.
const DefaultAgentVersion = "2.9.25"

const maxAgentVersionLength = 32

var agentVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// ValidAgentVersion allows omission (the pinned default) or a numeric X.Y.Z.
// It validates a URL/shell-safe release identifier, not availability or OS support.
func ValidAgentVersion(version string) bool {
	return version == "" || (len(version) <= maxAgentVersionLength && agentVersionPattern.MatchString(version))
}
