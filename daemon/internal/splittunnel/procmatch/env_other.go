//go:build !windows && !darwin && !linux

package procmatch

func hostSettings() envSettings {
	return envSettings{}
}
